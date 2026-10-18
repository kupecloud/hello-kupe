// Command todo-api serves the todo API, or with the "migrate" argument applies
// the schema migrations and exits.
//
// Both modes read the database from a binding Secret mounted as files. The
// Deployment mounts the readwrite role's Secret (data access only) and the
// migration Job mounts the owner role's Secret (schema changes), so the same
// image runs with different privileges.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/api"
	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/binding"
	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/config"
	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/migrate"
	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/store"
)

// maxConns per API pod. Every connection counts against the role's
// connection limit, which is shared by all replicas.
const maxConns = 4

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	err := run(ctx, os.Args[1:], logger)
	stop()
	if err != nil {
		logger.Error("todo-api failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	mode := "serve"
	if len(args) > 0 {
		mode = args[0]
	}
	if mode != "serve" && mode != "migrate" {
		return fmt.Errorf("unknown command %q: want serve or migrate", mode)
	}

	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	db, err := binding.ReadPostgres(cfg.BindingDir)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "database binding loaded",
		slog.String("mode", mode),
		slog.String("binding", cfg.BindingDir),
		slog.String("host", db.Host),
		slog.Int("port", db.Port),
		slog.String("database", db.Database),
		slog.String("username", db.Username),
	)

	if mode == "migrate" {
		return runMigrate(ctx, db.ConnString("todo-migrate"), logger)
	}
	password := func() (string, error) { return binding.ReadPassword(cfg.BindingDir) }
	return serve(ctx, cfg.Port, db.ConnString("todo-api"), password, logger)
}

func runMigrate(ctx context.Context, connString string, logger *slog.Logger) error {
	connCfg, err := pgx.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("parse connection config: %w", err)
	}
	sqlDB := stdlib.OpenDB(*connCfg)
	defer func() { _ = sqlDB.Close() }()
	return migrate.Up(ctx, sqlDB, logger)
}

func serve(ctx context.Context, port int, connString string, password func() (string, error), logger *slog.Logger) error {
	todos, err := store.Open(ctx, connString, maxConns, password)
	if err != nil {
		return err
	}
	defer todos.Close()

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           api.NewHandler(todos, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Closed once Shutdown has drained in-flight requests.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "shutdown failed", slog.String("error", err.Error()))
		}
	}()

	logger.InfoContext(ctx, "listening", slog.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	<-drained
	return nil
}

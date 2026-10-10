// Command todo-web serves the built frontend. It holds no credentials and
// makes no outbound calls: the browser talks to the API directly.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/static"
)

const (
	defaultPort = 8080
	defaultRoot = "/www"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx, logger)
	stop()
	if err != nil {
		logger.Error("todo-web failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	port := defaultPort
	if raw := os.Getenv("PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("PORT %q is not a TCP port", raw)
		}
		port = p
	}
	root := defaultRoot
	if v := os.Getenv("WEB_ROOT"); v != "" {
		root = v
	}
	files := os.DirFS(root)
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return fmt.Errorf("WEB_ROOT %s has no index.html: %w", root, err)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           static.Handler(files),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

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

	logger.InfoContext(ctx, "listening", slog.String("addr", srv.Addr), slog.String("root", root))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	<-drained
	return nil
}

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/kupecloud/hello-kupe/internal/binding"
	"github.com/kupecloud/hello-kupe/internal/game"
	"github.com/kupecloud/hello-kupe/internal/migrate"
)

func databaseConnection(cfg Config) (string, func() (string, error), error) {
	if cfg.DatabaseBinding != "" {
		if cfg.DatabaseURL != "" {
			return "", nil, errors.New("set DATABASE_BINDING or DATABASE_URL, not both")
		}
		dir, err := binding.Dir(cfg.BindingRoot, cfg.DatabaseBinding)
		if err != nil {
			return "", nil, err
		}
		details, err := binding.ReadPostgres(dir)
		if err != nil {
			return "", nil, err
		}
		return details.ConnString(cfg.ServiceName), func() (string, error) { return binding.ReadPassword(dir) }, nil
	}
	return cfg.DatabaseURL, nil, nil
}
func openGameStore(cfg Config) (game.Store, error) {
	uri, password, err := databaseConnection(cfg)
	if err != nil {
		return nil, err
	}
	if uri == "" {
		return game.NewMemory(), nil
	}
	return game.Open(context.Background(), uri, password)
}

// Migrate runs with the owner's binding in its own namespace, never inside the
// serving process. The serving process only needs readwrite privileges.
func Migrate(ctx context.Context, cfg Config, logger *slog.Logger) error {
	uri, _, err := databaseConnection(cfg)
	if err != nil {
		return err
	}
	if uri == "" {
		return errors.New("migration requires a database binding or DATABASE_URL")
	}
	config, err := pgx.ParseConfig(uri)
	if err != nil {
		return errors.New("invalid database connection configuration")
	}
	db := stdlib.OpenDB(*config)
	defer func() { _ = db.Close() }()
	if err = migrate.Up(ctx, db, logger); err != nil {
		return fmt.Errorf("migrate game schema: %w", err)
	}
	return nil
}

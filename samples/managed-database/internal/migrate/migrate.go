// Package migrate applies the embedded SQL migrations with goose.
//
// Running it again is safe: goose records applied versions in the
// goose_db_version table and skips them, runs each migration in its own
// transaction so a failure leaves nothing half-applied, and holds a
// PostgreSQL advisory lock for the whole run so two Jobs started together
// cannot apply the same migration twice.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var embedded embed.FS

// NewProvider returns a goose provider over the embedded migrations.
func NewProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}
	return provider, nil
}

// Up applies every pending migration and logs each one.
func Up(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	provider, err := NewProvider(db)
	if err != nil {
		return err
	}
	results, err := provider.Up(ctx)
	for _, r := range results {
		logger.InfoContext(ctx, "migration applied",
			slog.Int64("version", r.Source.Version),
			slog.String("file", r.Source.Path),
			slog.Duration("duration", r.Duration),
		)
	}
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	logger.InfoContext(ctx, "schema up to date",
		slog.Int64("version", version),
		slog.Int("applied", len(results)),
	)
	return nil
}

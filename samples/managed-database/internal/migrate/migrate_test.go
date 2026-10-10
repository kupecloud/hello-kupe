package migrate

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// The migrations are embedded at build time; this fails if a file is dropped
// from the embed pattern or cannot be parsed by goose.
func TestEmbeddedMigrationsParse(t *testing.T) {
	// sql.Open does not connect, so no database is needed to list sources.
	db, err := sql.Open("pgx", "postgres://unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider, err := NewProvider(db)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	sources := provider.ListSources()
	if len(sources) == 0 {
		t.Fatal("no migrations embedded")
	}
	if sources[0].Version != 1 {
		t.Fatalf("first migration version = %d, want 1", sources[0].Version)
	}
}

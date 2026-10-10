package store

import (
	"context"
	"errors"
	"testing"
)

// Nothing listens on port 1, and Open does not connect, so no database is
// needed to inspect the pool it builds.
const unusedConnString = "postgres://app_user:startup-password@127.0.0.1:1/todo_db?sslmode=disable"

func TestOpen_ReadsPasswordForEachNewConnection(t *testing.T) {
	passwords := []string{"first", "rotated"}
	calls := 0
	p, err := Open(context.Background(), unusedConnString, 2, func() (string, error) {
		current := passwords[calls]
		calls++
		return current, nil
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(p.Close)

	cfg := p.pool.Config()
	if cfg.BeforeConnect == nil {
		t.Fatal("BeforeConnect is not set: a rotated password would need a restart")
	}
	for _, want := range passwords {
		// pgxpool hands BeforeConnect a fresh copy of the parsed config for
		// every new connection.
		conn := cfg.ConnConfig.Copy()
		if err := cfg.BeforeConnect(context.Background(), conn); err != nil {
			t.Fatalf("BeforeConnect: %v", err)
		}
		if conn.Password != want {
			t.Fatalf("connection password = %q, want %q", conn.Password, want)
		}
	}
}

func TestOpen_PasswordReadFailureFailsTheConnection(t *testing.T) {
	errUnreadable := errors.New("binding unreadable")
	p, err := Open(context.Background(), unusedConnString, 2, func() (string, error) {
		return "", errUnreadable
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(p.Close)

	cfg := p.pool.Config()
	conn := cfg.ConnConfig.Copy()
	if err := cfg.BeforeConnect(context.Background(), conn); !errors.Is(err, errUnreadable) {
		t.Fatalf("BeforeConnect error = %v, want %v", err, errUnreadable)
	}
	if conn.Password != "startup-password" {
		t.Fatalf("password changed to %q after a failed read", conn.Password)
	}
}

func TestOpen_RecyclesConnections(t *testing.T) {
	p, err := Open(context.Background(), unusedConnString, 2, func() (string, error) { return "x", nil })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(p.Close)

	cfg := p.pool.Config()
	if cfg.MaxConnLifetime != maxConnLifetime || cfg.MaxConnLifetimeJitter != maxConnLifetimeJitter {
		t.Fatalf("MaxConnLifetime = %s (+%s jitter), want %s (+%s)",
			cfg.MaxConnLifetime, cfg.MaxConnLifetimeJitter, maxConnLifetime, maxConnLifetimeJitter)
	}
	if cfg.MaxConns != 2 {
		t.Fatalf("MaxConns = %d, want 2", cfg.MaxConns)
	}
}

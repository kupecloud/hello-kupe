package game

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// This test only runs against an explicitly supplied disposable local database,
// migrated with the owner role. Normal unit tests need no database.
func TestPostgresPersistence(t *testing.T) {
	uri := os.Getenv("TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	first, err := Open(ctx, uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(ctx, uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err = first.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	id, _, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Create(ctx, id, knownGame()); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = first.pool.Exec(ctx, "DELETE FROM kupe_games WHERE id=$1", id) }()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() { _, _ = second.Change(ctx, id, func(s *State) error { return s.Flip(0, time.Now()) }) })
	}
	wg.Wait()
	_, err = first.Change(ctx, id, func(s *State) error {
		if len(s.Selected) != 1 {
			t.Fatal("concurrent replica reveals were not serialised")
		}
		s.Name = "rolled back"
		return ErrInvalid
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("rollback expected")
	}
	now := time.Now()
	for i, index := range []int{2, 1, 3} {
		_, err = second.Change(ctx, id, func(s *State) error {
			if s.Name != "" {
				t.Fatal("transaction failed to roll back")
			}
			return s.Flip(index, now.Add(time.Duration(i)*time.Second))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = first.Change(ctx, id, func(s *State) error { s.Name = "Persistence check"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
	// A new pool represents another process after a restart.
	restarted, err := Open(ctx, uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	saved, err := restarted.Change(ctx, id, func(*State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Persistence check" || saved.Moves != 2 || saved.CompletedAt.IsZero() {
		t.Fatal("score did not survive a new connection pool")
	}
	scores, err := restarted.Scores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range scores {
		if s.Name == saved.Name && s.Moves == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("persisted score absent from leaderboard")
	}
	if _, err = first.pool.Exec(ctx, "SELECT * FROM goose_db_version"); err == nil {
		t.Fatal("readwrite role can access migration history")
	}
	if _, err = first.pool.Exec(ctx, "CREATE TABLE must_not_be_created (id int)"); err == nil {
		t.Fatal("readwrite role can create tables")
	}
}

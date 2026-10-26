package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store must serialise mutations of a session, including across replicas.
type Store interface {
	Create(context.Context, string, State) error
	Change(context.Context, string, func(*State) error) (State, error)
	Scores(context.Context) ([]Score, error)
	Ping(context.Context) error
	Close()
	Persistent() bool
}

type memoryEntry struct {
	state   State
	expires time.Time
}
type Memory struct {
	mu    sync.Mutex
	games map[string]memoryEntry
}

func NewMemory() *Memory                     { return &Memory{games: map[string]memoryEntry{}} }
func (m *Memory) Close()                     {}
func (m *Memory) Persistent() bool           { return false }
func (m *Memory) Ping(context.Context) error { return nil }
func (m *Memory) Create(_ context.Context, id string, s State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, entry := range m.games {
		if time.Now().After(entry.expires) {
			delete(m.games, key)
		}
	}
	if len(m.games) >= 2000 {
		return ErrCapacity
	}
	m.games[id] = memoryEntry{s, time.Now().Add(Lifetime)}
	return nil
}
func clone(s State) State {
	s.Deck = append([]string{}, s.Deck...)
	s.Matched = append([]bool{}, s.Matched...)
	s.Selected = append([]int{}, s.Selected...)
	return s
}
func (m *Memory) Change(_ context.Context, id string, fn func(*State) error) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.games[id]
	if !ok || time.Now().After(entry.expires) {
		return State{}, ErrNotFound
	}
	s := clone(entry.state)
	if err := fn(&s); err != nil {
		return State{}, err
	}
	entry.state = s
	m.games[id] = entry
	return clone(s), nil
}
func (m *Memory) Scores(context.Context) ([]Score, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	scores := []Score{}
	for _, entry := range m.games {
		if entry.state.Name != "" {
			scores = append(scores, Score{entry.state.Name, entry.state.Moves, entry.state.DurationMS})
		}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Moves != scores[j].Moves {
			return scores[i].Moves < scores[j].Moves
		}
		if scores[i].DurationMS != scores[j].DurationMS {
			return scores[i].DurationMS < scores[j].DurationMS
		}
		return scores[i].Name < scores[j].Name
	})
	return scores[:min(len(scores), 10)], nil
}

type Postgres struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, uri string, password func() (string, error)) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(uri)
	if err != nil {
		return nil, errors.New("invalid database connection configuration")
	}
	cfg.MaxConns = 4
	cfg.MaxConnLifetime = 15 * time.Minute
	cfg.MaxConnLifetimeJitter = time.Minute
	if password != nil {
		cfg.BeforeConnect = func(_ context.Context, c *pgx.ConnConfig) error {
			value, err := password()
			if err != nil {
				return err
			}
			c.Password = value
			return nil
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Postgres{pool}, nil
}
func (p *Postgres) Close()           { p.pool.Close() }
func (p *Postgres) Persistent() bool { return true }
func (p *Postgres) Ping(ctx context.Context) error {
	var exists bool
	// Readiness also confirms migrations have created the application table.
	return p.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM kupe_games LIMIT 1)").Scan(&exists)
}
func (p *Postgres) Create(ctx context.Context, id string, s State) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// Expired unfinished sessions are removed in small batches as new games start.
	// Submitted scores remain until the demo's operator removes them.
	_, err = p.pool.Exec(ctx, `WITH expired AS (
 SELECT id FROM kupe_games WHERE expires_at < now() AND COALESCE(state->>'name','')='' LIMIT 100
 ) DELETE FROM kupe_games WHERE id IN (SELECT id FROM expired)`)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, "INSERT INTO kupe_games (id,state,expires_at) VALUES ($1,$2,$3)", id, raw, time.Now().Add(Lifetime))
	return err
}
func (p *Postgres) Change(ctx context.Context, id string, fn func(*State) error) (State, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT state FROM kupe_games WHERE id=$1 AND expires_at>now() FOR UPDATE", id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, ErrNotFound
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, err
	}
	if err := fn(&s); err != nil {
		return State{}, err
	}
	raw, err = json.Marshal(s)
	if err != nil {
		return State{}, err
	}
	if _, err = tx.Exec(ctx, "UPDATE kupe_games SET state=$2 WHERE id=$1", id, raw); err != nil {
		return State{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return State{}, err
	}
	return s, nil
}
func (p *Postgres) Scores(ctx context.Context) ([]Score, error) {
	rows, err := p.pool.Query(ctx, `SELECT state->>'name',(state->>'moves')::integer,(state->>'duration_ms')::bigint
 FROM kupe_games WHERE COALESCE(state->>'name','')<>''
 ORDER BY (state->>'moves')::integer,(state->>'duration_ms')::bigint,id LIMIT 10`)
	if err != nil {
		return nil, fmt.Errorf("read leaderboard: %w", err)
	}
	defer rows.Close()
	scores := []Score{}
	for rows.Next() {
		var s Score
		if err := rows.Scan(&s.Name, &s.Moves, &s.DurationMS); err != nil {
			return nil, err
		}
		scores = append(scores, s)
	}
	return scores, rows.Err()
}

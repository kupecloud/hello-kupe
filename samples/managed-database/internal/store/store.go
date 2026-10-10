// Package store implements api.Store on PostgreSQL with pgx.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/api"
)

// listLimit bounds GET /api/todos so one request cannot read an unbounded table.
const listLimit = 500

const columns = "id, title, done, created_at, updated_at"

// maxConnLifetime closes each connection after about this long. PostgreSQL
// keeps a session open after its role's password is rotated, so recycling
// bounds how long a connection made with the old password lives and makes
// every pod log in again with the current one regularly.
const (
	maxConnLifetime       = 15 * time.Minute
	maxConnLifetimeJitter = time.Minute
)

// Postgres is a Store backed by a connection pool.
type Postgres struct {
	pool *pgxpool.Pool
}

// Open creates a pool from a connection string. password is called for every
// new connection and its result replaces the password in connString, so a
// rotated password is used without a restart.
//
// Open does not connect: connections are made on first use, and /readyz
// reports whether the database is reachable. The caller must Close it.
func Open(ctx context.Context, connString string, maxConns int32, password func() (string, error)) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse connection config: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.MaxConnLifetimeJitter = maxConnLifetimeJitter
	cfg.BeforeConnect = func(_ context.Context, conn *pgx.ConnConfig) error {
		current, err := password()
		if err != nil {
			return fmt.Errorf("read database password: %w", err)
		}
		conn.Password = current
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

// Close releases every connection.
func (p *Postgres) Close() { p.pool.Close() }

// Ping checks a connection can be acquired and the server answers.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p *Postgres) List(ctx context.Context) ([]api.Todo, error) {
	rows, err := p.pool.Query(ctx, "SELECT "+columns+" FROM todos ORDER BY id LIMIT $1", listLimit)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	todos, err := pgx.CollectRows(rows, scanTodo)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	return todos, nil
}

func (p *Postgres) Get(ctx context.Context, id int64) (api.Todo, error) {
	return p.one(ctx, "get todo", "SELECT "+columns+" FROM todos WHERE id = $1", id)
}

func (p *Postgres) Create(ctx context.Context, title string) (api.Todo, error) {
	return p.one(ctx, "create todo", "INSERT INTO todos (title) VALUES ($1) RETURNING "+columns, title)
}

func (p *Postgres) Update(ctx context.Context, id int64, patch api.Patch) (api.Todo, error) {
	return p.one(ctx, "update todo",
		`UPDATE todos
		    SET title = COALESCE($2, title), done = COALESCE($3, done), updated_at = now()
		  WHERE id = $1
		RETURNING `+columns,
		id, patch.Title, patch.Done)
}

func (p *Postgres) Delete(ctx context.Context, id int64) error {
	tag, err := p.pool.Exec(ctx, "DELETE FROM todos WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete todo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return api.ErrNotFound
	}
	return nil
}

func (p *Postgres) one(ctx context.Context, op, query string, args ...any) (api.Todo, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return api.Todo{}, fmt.Errorf("%s: %w", op, err)
	}
	todo, err := pgx.CollectExactlyOneRow(rows, scanTodo)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.Todo{}, api.ErrNotFound
	}
	if err != nil {
		return api.Todo{}, fmt.Errorf("%s: %w", op, err)
	}
	return todo, nil
}

func scanTodo(row pgx.CollectableRow) (api.Todo, error) {
	var t api.Todo
	err := row.Scan(&t.ID, &t.Title, &t.Done, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

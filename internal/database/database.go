// Package database manages the PostgreSQL connection pool.
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrMissingURL is returned when no connection URL is provided.
var ErrMissingURL = errors.New("database: connection URL is empty (set DATABASE_URL)")

// Connect opens a connection pool to PostgreSQL and checks that the server
// answers. The caller must Close the returned pool.
//
// url follows the libpq format, e.g.
// postgres://user:password@localhost:5432/dbname?sslmode=disable
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, ErrMissingURL
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return pool, nil
}

// ServerVersion returns the PostgreSQL server version string.
func ServerVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var version string
	if err := pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return "", fmt.Errorf("database: query version: %w", err)
	}
	return version, nil
}

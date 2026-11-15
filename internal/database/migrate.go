package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationLockID is an arbitrary key for the PostgreSQL advisory lock that
// stops two instances from migrating at the same time.
const migrationLockID = 727272

// Migrate applies, in name order, every SQL file in migrations/ that has not
// been applied yet. Each file runs in its own transaction and is recorded in
// the schema_migrations table. It returns the names of the files applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("database: migrate: acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return nil, fmt.Errorf("database: migrate: lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("database: migrate: create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("database: migrate: list files: %w", err)
	}
	sort.Strings(names)

	var applied []string
	for _, path := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(path, "migrations/"), ".sql")

		var done bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version,
		).Scan(&done); err != nil {
			return applied, fmt.Errorf("database: migrate: check %s: %w", version, err)
		}
		if done {
			continue
		}

		sql, err := migrationsFS.ReadFile(path)
		if err != nil {
			return applied, fmt.Errorf("database: migrate: read %s: %w", version, err)
		}

		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
			return err
		}); err != nil {
			return applied, fmt.Errorf("database: migrate: apply %s: %w", version, err)
		}
		applied = append(applied, version)
	}

	return applied, nil
}

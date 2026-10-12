// Package store owns Go's own PostgreSQL data: the connection pool and goose migrations.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open creates a pool for url and verifies connectivity.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("store: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return pool, nil
}

// RequireCurrentSchema checks every embedded version without creating any table.
// Explicit local activation uses this to prevent startup from applying migrations.
func RequireCurrentSchema(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&exists); err != nil {
		return fmt.Errorf("store: schema check: %w", err)
	}
	if !exists {
		return fmt.Errorf("store: schema must be explicitly provisioned before startup")
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	for _, file := range files {
		version, err := strconv.ParseInt(strings.Split(strings.TrimPrefix(file, "migrations/"), "_")[0], 10, 64)
		if err != nil {
			return fmt.Errorf("store: invalid embedded migration version")
		}
		var applied bool
		if err := pool.QueryRow(ctx, "SELECT is_applied FROM goose_db_version WHERE version_id=$1 ORDER BY id DESC LIMIT 1", version).Scan(&applied); err != nil || !applied {
			return fmt.Errorf("store: migration %d must be explicitly provisioned before startup", version)
		}
	}
	return nil
}

// Migrate applies all pending embedded migrations; it is safe to call repeatedly
// and from several processes at once: a session-level advisory lock (probed
// every second for up to 5 minutes) serialises concurrent callers.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fsSub()
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300))
	if err != nil {
		return fmt.Errorf("store: migration lock: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }() // releases the sql.DB wrapper only; the pool stays open
	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("store: goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

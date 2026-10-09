// Package storetest provides isolated PostgreSQL schemas for tests, backed by one
// testcontainers postgres:18-alpine container per test binary.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/goleak"

	"github.com/atipongsena/erp-town/go/internal/store"
)

var (
	once    sync.Once
	baseURL string
	errBase error
)

// IgnoreBackgroundGoroutines lists goleak options for goroutines that testcontainers and
// pgx keep alive for the whole test binary.
func IgnoreBackgroundGoroutines() []goleak.Option {
	return []goleak.Option{
		goleak.IgnoreAnyFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
		goleak.IgnoreAnyFunction("github.com/testcontainers/testcontainers-go.(*Reaper).Connect.func1"),
		goleak.IgnoreAnyFunction("github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck"),
		goleak.IgnoreAnyFunction("github.com/jackc/pgx/v5/pgxpool.(*Pool).triggerHealthCheck.func1"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreAnyFunction("github.com/Microsoft/go-winio.ioCompletionProcessor"),
	}
}

func start() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("town"),
		tcpostgres.WithUsername("town"),
		tcpostgres.WithPassword("town"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		return "", err
	}
	return c.ConnectionString(ctx, "sslmode=disable")
}

// NewPool returns a pool whose connections use a fresh, migrated schema that is dropped
// when the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := NewEmptyPool(t)
	if err := store.Migrate(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

// NewEmptyPool is NewPool without running the migrations.
func NewEmptyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	once.Do(func() { baseURL, errBase = start() })
	if errBase != nil {
		t.Fatalf("docker unavailable: start Docker Desktop (%v)", errBase)
	}
	ctx := t.Context()

	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	schema := "t_" + hex.EncodeToString(raw[:])

	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}

	cfg, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		pool.Close()
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(bg, fmt.Sprintf("DROP SCHEMA %s CASCADE", schema)); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		admin.Close()
	})
	return pool
}

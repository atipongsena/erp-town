// Package eventstest provides isolated Redis streams for tests, backed by one
// testcontainers redis:7-alpine container per test binary.
package eventstest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var (
	once    sync.Once
	baseURL string
	errBase error
)

func start() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		return "", err
	}
	return c.ConnectionString(ctx)
}

// NewRedis returns a client for the shared test container and a fresh stream name
// "test:<random hex>". The stream is deleted and the client closed when the test ends.
func NewRedis(t *testing.T) (*redis.Client, string) {
	t.Helper()
	once.Do(func() { baseURL, errBase = start() })
	if errBase != nil {
		t.Fatalf("redis test container: is Docker Desktop running? (%v)", errBase)
	}
	opts, err := redis.ParseURL(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)

	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	stream := "test:" + hex.EncodeToString(raw[:])

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := rdb.Del(ctx, stream).Err(); err != nil {
			t.Errorf("delete stream %s: %v", stream, err)
		}
		if err := rdb.Close(); err != nil {
			t.Errorf("close redis client: %v", err)
		}
	})
	return rdb, stream
}

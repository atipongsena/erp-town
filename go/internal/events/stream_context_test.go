package events

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestConsumerCarriesExactStreamEntryID(t *testing.T) {
	h := newHarness(t)
	h.createGroupFromStart(t)
	id, err := h.rdb.XAdd(t.Context(), &redis.XAddArgs{Stream: h.stream, Values: map[string]any{"event": string(testEvent(t, "SO-CURSOR", 1, nil))}}).Result()
	require.NoError(t, err)
	seen := ""
	c := NewConsumer(h.rdb, h.pool, h.v, h.m, h.config(), func(ctx context.Context, _ pgx.Tx, _ Envelope) error { seen = StreamID(ctx); return nil })
	require.NoError(t, c.step(t.Context()))
	require.Equal(t, id, seen)
	require.Empty(t, StreamID(context.Background()))
}

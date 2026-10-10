package events

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerHandlerPanicRollsBackAndRetries(t *testing.T) {
	h := newHarness(t)
	_, err := h.pool.Exec(t.Context(), "CREATE TABLE handler_writes (attempt int NOT NULL)")
	require.NoError(t, err)
	h.createGroupFromStart(t)
	h.add(t, testEvent(t, "SO-PANIC", 1, nil))
	attempts := 0
	cfg := h.config()
	cfg.ClaimIdle = time.Millisecond
	c := NewConsumer(h.rdb, h.pool, h.v, h.m, cfg, func(ctx context.Context, tx pgx.Tx, _ Envelope) error {
		// Release the transaction even when testing the original unhandled panic.
		// Assertions below run before cleanup, so this cannot hide a missing rollback.
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		attempts++
		_, err := tx.Exec(ctx, "INSERT INTO handler_writes VALUES ($1)", attempts)
		if err != nil {
			return err
		}
		if attempts == 1 {
			panic("failure after writing")
		}
		return nil
	})

	require.NotPanics(t, func() { require.NoError(t, c.step(t.Context())) })
	assert.Equal(t, 0, h.count(t, "SELECT count(*) FROM handler_writes"))
	assert.Equal(t, 0, h.count(t, "SELECT count(*) FROM consumed_events"))
	assert.Equal(t, 0, h.count(t, "SELECT count(*) FROM consumer_subject_seq"))
	pending, err := h.rdb.XPending(t.Context(), h.stream, testGroup).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending.Count)

	require.EventuallyWithT(t, func(a *assert.CollectT) {
		require.NoError(a, c.reclaim(t.Context()))
		assert.Equal(a, 2, attempts)
	}, 5*time.Second, 5*time.Millisecond)
	h.waitDrained(t)
	assert.Equal(t, 1, h.count(t, "SELECT count(*) FROM handler_writes WHERE attempt = 2"))
	assert.Equal(t, 1, h.count(t, "SELECT count(*) FROM consumed_events"))
}

func TestConsumerPersistentPanicDropsAndContinues(t *testing.T) {
	h := newHarness(t)
	h.createGroupFromStart(t)
	h.add(t, testEvent(t, "SO-POISON", 1, nil))
	h.add(t, testEvent(t, "SO-HEALTHY", 1, nil))
	cfg := h.config()
	cfg.ClaimIdle = time.Millisecond
	cfg.MaxDeliveries = 2
	attempts, healthy := 0, 0
	c := NewConsumer(h.rdb, h.pool, h.v, h.m, cfg, func(_ context.Context, tx pgx.Tx, e Envelope) error {
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		if e.Subject.Name == "SO-POISON" {
			attempts++
			panic(nil)
		}
		healthy++
		return nil
	})
	require.NotPanics(t, func() { require.NoError(t, c.step(t.Context())) })
	assert.Equal(t, 1, healthy, "the rest of the batch still runs")
	require.EventuallyWithT(t, func(a *assert.CollectT) {
		require.NoError(a, c.reclaim(t.Context()))
		assert.Equal(a, int64(1), h.read("town_events_dropped_total"))
	}, 5*time.Second, 5*time.Millisecond)
	h.waitDrained(t)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 1, h.count(t, "SELECT count(*) FROM consumed_events"))
}

func TestConsumerStaleSeqIsCountedAndLoggedOnce(t *testing.T) {
	h := newHarness(t)
	rec := &recorder{}
	c := NewConsumer(h.rdb, h.pool, h.v, h.m, h.config(), rec.handle)
	var logs bytes.Buffer
	c.log = slog.New(slog.NewJSONHandler(&logs, nil))
	latest, err := Decode(h.v, testEvent(t, "SO-STALE", 3, nil))
	require.NoError(t, err)
	stale, err := Decode(h.v, testEvent(t, "SO-STALE", 2, nil))
	require.NoError(t, err)
	require.NoError(t, c.apply(t.Context(), latest))
	require.NoError(t, c.apply(t.Context(), stale))
	require.NoError(t, c.apply(t.Context(), stale))
	assert.Equal(t, []int64{3}, rec.seqs("SO-STALE"))
	assert.Equal(t, int64(1), h.read("town_events_stale_total"))
	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry))
	assert.Equal(t, stale.ID, entry["event_id"])
	assert.Equal(t, "SO-STALE", entry["subject_name"])
	assert.Equal(t, "Sales Order", entry["subject_doctype"])
	assert.Equal(t, float64(2), entry["seq"])
	assert.Equal(t, float64(3), entry["last_seq"])
	assert.Equal(t, testGroup, entry["group"])
}

package events

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/events/eventstest"
	"github.com/atipongsena/erp-town/go/internal/events/gen"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

type pubHarness struct {
	pool   *pgxpool.Pool
	rdb    *redis.Client
	stream string
	v      *Validator
	m      *platform.Metrics
	read   func(string) int64
}

func newPubHarness(t *testing.T) *pubHarness {
	t.Helper()
	rdb, stream := eventstest.NewRedis(t)
	m, read := platform.NewTestMetrics()
	return &pubHarness{pool: storetest.NewPool(t), rdb: rdb, stream: stream, v: newValidator(t), m: m, read: read}
}

func (h *pubHarness) config(now time.Time) PublisherConfig {
	return PublisherConfig{Stream: h.stream, RetentionDays: 7, MaxAttempts: 10, BatchSize: 100, Now: func() time.Time { return now }}
}

func (h *pubHarness) publisher(rdb *redis.Client, now time.Time) *Publisher {
	return NewPublisher(h.pool, rdb, h.v, h.m, h.config(now))
}

// downRedis returns a client for a port nothing listens on.
func downRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 2 * time.Second})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// insertRaw stores a Pending row directly (a type Go has no emitter for yet).
func (h *pubHarness) insertRaw(t *testing.T, typ string, attempts int) string {
	t.Helper()
	id := ulid.Make().String()
	_, err := h.pool.Exec(t.Context(), `
		INSERT INTO town_outbox (id, type, subject_doctype, subject_name, payload, status, attempts, created_at)
		VALUES ($1, $2, 'Agent Run', 'RUN-1', $3, 'Pending', $4, $5)`,
		id, typ, `{"id":"`+id+`"}`, attempts, testNow)
	require.NoError(t, err)
	return id
}

func TestPublishWritesEventFieldByteExact(t *testing.T) {
	h := newPubHarness(t)
	id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "ทดสอบ & <ok>"}))
	stored := loadRow(t, h.pool, id).Payload
	require.Contains(t, stored, "ทดสอบ & <ok>")

	n, err := h.publisher(h.rdb, testNow).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	msgs, err := h.rdb.XRange(t.Context(), h.stream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, map[string]any{"event": stored}, msgs[0].Values)

	r := loadRow(t, h.pool, id)
	assert.Equal(t, "Published", r.Status)
	require.NotNil(t, r.StreamID)
	assert.Equal(t, msgs[0].ID, *r.StreamID)
	require.NotNil(t, r.PublishedAt)
	assert.True(t, testNow.Equal(*r.PublishedAt))
	assert.Nil(t, r.NextAttemptAt)
	assert.Zero(t, h.read(platform.MetricOutboxPublishFailures))

	n, err = h.publisher(h.rdb, testNow).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n, "published rows are not published again")
}

func TestPublishBacksOffWhenRedisDown(t *testing.T) {
	h := newPubHarness(t)
	id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "x"}))

	down := h.publisher(downRedis(t), testNow)
	n, err := down.PublishDue(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n)

	r := loadRow(t, h.pool, id)
	assert.Equal(t, "Pending", r.Status)
	assert.Equal(t, 1, r.Attempts)
	require.NotNil(t, r.LastError)
	assert.NotEmpty(t, *r.LastError)
	require.NotNil(t, r.NextAttemptAt)
	assert.True(t, testNow.Add(2*time.Second).Equal(*r.NextAttemptAt), "next_attempt_at = %v", *r.NextAttemptAt)
	assert.Equal(t, int64(1), h.read(platform.MetricOutboxPublishFailures))

	// Not due yet: nothing is tried, even with Redis back.
	n, err = h.publisher(h.rdb, testNow.Add(time.Second)).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, 1, loadRow(t, h.pool, id).Attempts)

	// Due: published.
	n, err = h.publisher(h.rdb, testNow.Add(2*time.Second)).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	r = loadRow(t, h.pool, id)
	assert.Equal(t, "Published", r.Status)
	assert.Nil(t, r.LastError)
}

func TestPublishBackoffIsCappedAt300s(t *testing.T) {
	h := newPubHarness(t)
	id := h.insertRaw(t, "agent.run.finished", 8) // 9th attempt: 2^9 = 512 > 300
	_, err := h.publisher(downRedis(t), testNow).PublishDue(t.Context())
	require.NoError(t, err)
	r := loadRow(t, h.pool, id)
	assert.Equal(t, "Pending", r.Status)
	assert.Equal(t, 9, r.Attempts)
	require.NotNil(t, r.NextAttemptAt)
	assert.True(t, testNow.Add(300*time.Second).Equal(*r.NextAttemptAt), "next_attempt_at = %v", *r.NextAttemptAt)
}

func TestPublishFailsAtMaxAttemptsAndReports(t *testing.T) {
	h := newPubHarness(t)
	id := h.insertRaw(t, "agent.run.finished", 9)

	n, err := h.publisher(downRedis(t), testNow).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n)

	r := loadRow(t, h.pool, id)
	assert.Equal(t, "Failed", r.Status)
	assert.Equal(t, 10, r.Attempts)
	assert.Nil(t, r.NextAttemptAt)
	require.NotNil(t, r.LastError)
	assert.Equal(t, int64(1), h.read(platform.MetricOutboxPublishFailures))

	rows := loadRows(t, h.pool)
	require.Len(t, rows, 2)
	var report outboxRow
	for _, row := range rows {
		if row.ID != id {
			report = row
		}
	}
	assert.Equal(t, "Pending", report.Status)
	assert.Equal(t, "system.error", report.Type)
	e := decodeStored(t, h.v, report.Payload)
	data, err := DecodeData[gen.SystemErrorData](e)
	require.NoError(t, err)
	assert.Equal(t, OutboxPublishFailed, data.Code)
	assert.Contains(t, data.Message, id)
	assert.Equal(t, Subject{DocType: "Town Event Outbox", Name: id}, e.Subject)
	require.NotNil(t, e.CausationID)
	assert.Equal(t, id, *e.CausationID)
	assert.Equal(t, SystemErrorSource, e.Source)
	assert.Equal(t, SystemActor(), e.Actor)
	assert.Nil(t, e.Company)
	assert.Equal(t, Visibility{Level: "roles", Roles: []string{"System Manager"}}, e.Visibility)
}

func TestFailedSystemErrorDoesNotReport(t *testing.T) {
	h := newPubHarness(t)
	id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "x"}))
	_, err := h.pool.Exec(t.Context(), "UPDATE town_outbox SET attempts = 9 WHERE id = $1", id)
	require.NoError(t, err)

	n, err := h.publisher(downRedis(t), testNow).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n)

	rows := loadRows(t, h.pool)
	require.Len(t, rows, 1, "a failed system.error is not reported with another system.error")
	assert.Equal(t, "Failed", rows[0].Status)
	assert.Equal(t, 10, rows[0].Attempts)
}

func TestPublishSkipsLockedRows(t *testing.T) {
	h := newPubHarness(t)
	locked := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("A", gen.SystemErrorData{Code: "TEST", Message: "a"}))
	free := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("B", gen.SystemErrorData{Code: "TEST", Message: "b"}))

	inTx(t, h.pool, func(tx pgx.Tx) {
		_, err := tx.Exec(t.Context(), "SELECT 1 FROM town_outbox WHERE id = $1 FOR UPDATE", locked)
		require.NoError(t, err)
		n, err := h.publisher(h.rdb, testNow).PublishDue(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})
	assert.Equal(t, "Pending", loadRow(t, h.pool, locked).Status)
	assert.Equal(t, "Published", loadRow(t, h.pool, free).Status)
}

func TestPublishHonoursBatchSizeAndOrder(t *testing.T) {
	h := newPubHarness(t)
	var ids []string
	for i := range 3 {
		ids = append(ids, emitCommitted(t, h.pool, h.v, testNow.Add(time.Duration(i)*time.Millisecond),
			systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "m"})))
	}
	cfg := h.config(testNow.Add(time.Second))
	cfg.BatchSize = 2
	n, err := NewPublisher(h.pool, h.rdb, h.v, h.m, cfg).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, "Published", loadRow(t, h.pool, ids[0]).Status)
	assert.Equal(t, "Published", loadRow(t, h.pool, ids[1]).Status)
	assert.Equal(t, "Pending", loadRow(t, h.pool, ids[2]).Status)

	msgs, err := h.rdb.XRange(t.Context(), h.stream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, ids[0], decodeStored(t, h.v, msgs[0].Values["event"].(string)).ID)
	assert.Equal(t, ids[1], decodeStored(t, h.v, msgs[1].Values["event"].(string)).ID)
}

// The publish order follows the order rows were stored after the subject's seq lock was
// taken, not the caller's now: an emitter whose now is earlier but that takes the seq
// lock later must not overtake (the consumer would drop the lower seq as stale).
func TestPublishOrderFollowsSeqNotCallerNow(t *testing.T) {
	h := newPubHarness(t)
	in := systemErrorInput("SUBJ", gen.SystemErrorData{Code: "TEST", Message: "m"})

	early, err := h.pool.Begin(t.Context()) // took its now first...
	require.NoError(t, err)
	defer func() { _ = early.Rollback(context.Background()) }()

	// ...but a later emitter takes seq 1 and commits first.
	emitCommitted(t, h.pool, h.v, testNow.Add(time.Minute), in)

	_, err = Emit(t.Context(), early, h.v, testNow, in)
	require.NoError(t, err)
	require.NoError(t, early.Commit(t.Context()))

	n, err := h.publisher(h.rdb, testNow.Add(time.Hour)).PublishDue(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, n)

	msgs, err := h.rdb.XRange(t.Context(), h.stream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	var seqs []int64
	for _, m := range msgs {
		seqs = append(seqs, decodeStored(t, h.v, m.Values["event"].(string)).Seq)
	}
	assert.Equal(t, []int64{1, 2}, seqs)
}

func TestPublishTrimsByRetention(t *testing.T) {
	h := newPubHarness(t)
	// 100 entries older than the 7-day retention fill one stream node (stream-node-max-entries
	// defaults to 100); MINID ~ trims whole nodes only, so that node is dropped once the
	// publisher's entry starts the next one.
	oldMs := time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	pipe := h.rdb.Pipeline()
	for i := range 100 {
		pipe.XAdd(t.Context(), &redis.XAddArgs{
			Stream: h.stream, ID: strconv.FormatInt(oldMs, 10) + "-" + strconv.Itoa(i+1), Values: []any{"event", "old"},
		})
	}
	_, err := pipe.Exec(t.Context())
	require.NoError(t, err)

	emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "x"}))
	n, err := NewPublisher(h.pool, h.rdb, h.v, h.m, h.config(time.Now())).PublishDue(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	left, err := h.rdb.XLen(t.Context(), h.stream).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), left, "the node of entries past retention is trimmed")
}

func TestRunPublishesUntilCancelled(t *testing.T) {
	h := newPubHarness(t)
	id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "x"}))

	p := h.publisher(h.rdb, testNow)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx, 20*time.Millisecond) }()

	require.Eventually(t, func() bool { return loadRow(t, h.pool, id).Status == "Published" }, 10*time.Second, 20*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop")
	}
}

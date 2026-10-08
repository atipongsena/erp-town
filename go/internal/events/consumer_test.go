package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

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

const testGroup = "world"

// recorder is a Handler that remembers every envelope it was called with.
type recorder struct {
	mu   sync.Mutex
	seen []Envelope
	err  error
}

func (r *recorder) handle(_ context.Context, e Envelope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, e)
	return r.err
}

func (r *recorder) calls() []Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Envelope(nil), r.seen...)
}

func (r *recorder) seqs(subject string) []int64 {
	var out []int64
	for _, e := range r.calls() {
		if e.Subject.Name == subject {
			out = append(out, e.Seq)
		}
	}
	return out
}

// harness wires one stream, one schema and test metrics for a consumer test.
type harness struct {
	rdb    *redis.Client
	stream string
	pool   *pgxpool.Pool
	v      *Validator
	m      *platform.Metrics
	read   func(string) int64
	lastID string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	rdb, stream := eventstest.NewRedis(t)
	m, read := platform.NewTestMetrics()
	return &harness{rdb: rdb, stream: stream, pool: storetest.NewPool(t), v: newValidator(t), m: m, read: read}
}

func (h *harness) config() ConsumerConfig {
	cfg := DefaultConsumerConfig(h.stream, testGroup, "c1")
	cfg.Block = 50 * time.Millisecond
	return cfg
}

func (h *harness) add(t *testing.T, raw []byte) {
	t.Helper()
	id, err := h.rdb.XAdd(t.Context(), &redis.XAddArgs{Stream: h.stream, Values: map[string]any{"event": raw}}).Result()
	require.NoError(t, err)
	h.lastID = id
}

// createGroupFromStart creates the consumer group at the beginning of the stream, so
// entries added before Run starts are delivered (Run itself creates missing groups at $).
func (h *harness) createGroupFromStart(t *testing.T) {
	t.Helper()
	require.NoError(t, h.rdb.XGroupCreateMkStream(t.Context(), h.stream, testGroup, "0").Err())
}

// run starts the consumer in the background; the test's cleanup stops it and checks
// that Run returned nil.
func (h *harness) run(t *testing.T, cfg ConsumerConfig, handler Handler) {
	t.Helper()
	c := NewConsumer(h.rdb, h.pool, h.v, h.m, cfg, handler)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(30 * time.Second):
			t.Error("consumer did not stop")
		}
	})
}

func (h *harness) groupInfo(t *testing.T) (redis.XInfoGroup, bool) {
	t.Helper()
	groups, err := h.rdb.XInfoGroups(context.Background(), h.stream).Result()
	if err != nil {
		return redis.XInfoGroup{}, false
	}
	for _, g := range groups {
		if g.Name == testGroup {
			return g, true
		}
	}
	return redis.XInfoGroup{}, false
}

// waitDrained waits until the group has read up to the last added entry and has
// nothing pending.
func (h *harness) waitDrained(t *testing.T) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		g, ok := h.groupInfo(t)
		require.True(c, ok, "group missing")
		assert.Equal(c, h.lastID, g.LastDeliveredID)
		assert.Equal(c, int64(0), g.Pending)
	}, 20*time.Second, 20*time.Millisecond)
}

func (h *harness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, h.pool.QueryRow(t.Context(), query, args...).Scan(&n))
	return n
}

// testEvent builds a valid event from the sales order fixture with a fresh ULID id,
// the given subject name and seq; edit may change the decoded data before encoding.
func testEvent(t *testing.T, subject string, seq int64, edit func(map[string]any)) []byte {
	t.Helper()
	v := newValidator(t)
	e, err := Decode(v, fixtures(t, "valid")["erp.sales_order.submitted.json"])
	require.NoError(t, err)
	e.ID = ulid.Make().String()
	e.Subject.Name = subject
	e.CorrelationID = subject
	e.Seq = seq
	if edit != nil {
		var data map[string]any
		require.NoError(t, json.Unmarshal(e.Data, &data))
		edit(data)
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		require.NoError(t, enc.Encode(data))
		e.Data = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	}
	raw, err := Marshal(e)
	require.NoError(t, err)
	require.NoError(t, v.Validate(raw))
	return raw
}

func TestDefaultConsumerConfig(t *testing.T) {
	cfg := DefaultConsumerConfig("town:events", "world", "town-1")
	assert.Equal(t, ConsumerConfig{
		Stream: "town:events", Group: "world", Name: "town-1",
		Block: 5 * time.Second, ClaimIdle: 60 * time.Second, MaxDeliveries: 5,
		DedupeWindow: 8 * 24 * time.Hour,
	}, cfg)
}

func TestConsumerAppliesInSeqOrderAndAcks(t *testing.T) {
	h := newHarness(t)
	h.createGroupFromStart(t)
	for seq := int64(1); seq <= 3; seq++ {
		h.add(t, testEvent(t, "SO-A", seq, nil))
	}
	rec := &recorder{}
	h.run(t, h.config(), rec.handle)
	h.waitDrained(t)

	assert.Equal(t, []int64{1, 2, 3}, rec.seqs("SO-A"))
	pending, err := h.rdb.XPending(t.Context(), h.stream, testGroup).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), pending.Count)
	assert.Equal(t, 3, h.count(t, "SELECT count(*) FROM consumed_events WHERE consumer_group = $1", testGroup))
	assert.Equal(t, 3, h.count(t,
		"SELECT last_seq FROM consumer_subject_seq WHERE consumer_group = $1 AND subject_doctype = 'Sales Order' AND subject_name = 'SO-A'",
		testGroup))
}

func TestConsumerRunsHandlerOnceForDuplicateID(t *testing.T) {
	h := newHarness(t)
	h.createGroupFromStart(t)
	raw := testEvent(t, "SO-D", 1, nil)
	h.add(t, raw)
	h.add(t, raw)
	rec := &recorder{}
	h.run(t, h.config(), rec.handle)
	h.waitDrained(t)

	assert.Len(t, rec.calls(), 1)
}

func TestConsumerSkipsStaleSeqButNeverWaits(t *testing.T) {
	h := newHarness(t)
	h.createGroupFromStart(t)
	h.add(t, testEvent(t, "SO-STALE", 2, nil))
	h.add(t, testEvent(t, "SO-STALE", 1, nil))
	h.add(t, testEvent(t, "SO-GAP", 1, nil))
	h.add(t, testEvent(t, "SO-GAP", 3, nil))
	rec := &recorder{}
	h.run(t, h.config(), rec.handle)
	h.waitDrained(t)

	assert.Equal(t, []int64{2}, rec.seqs("SO-STALE"))
	assert.Equal(t, []int64{1, 3}, rec.seqs("SO-GAP"))
	// the skipped event is still recorded as consumed, so a redelivery is deduped
	assert.Equal(t, 4, h.count(t, "SELECT count(*) FROM consumed_events WHERE consumer_group = $1", testGroup))
}

func TestConsumerAcksSchemaInvalid(t *testing.T) {
	h := newHarness(t)
	h.createGroupFromStart(t)
	h.add(t, []byte("{}"))
	h.add(t, fixtures(t, "invalid")["seq-zero.json"])
	rec := &recorder{}
	h.run(t, h.config(), rec.handle)
	h.waitDrained(t)

	assert.Empty(t, rec.calls())
	assert.Equal(t, int64(2), h.read("town_events_invalid_total"))
	assert.Equal(t, int64(0), h.read("town_events_dropped_total"))
}

func TestConsumerDropsAfterMaxDeliveries(t *testing.T) {
	h := newHarness(t)
	h.createGroupFromStart(t)
	raw := testEvent(t, "SO-FAIL", 1, nil)
	h.add(t, raw)
	rec := &recorder{err: errors.New("boom")}
	cfg := h.config()
	cfg.ClaimIdle = 50 * time.Millisecond
	cfg.MaxDeliveries = 5
	h.run(t, cfg, rec.handle)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, int64(1), h.read("town_events_dropped_total"))
		g, ok := h.groupInfo(t)
		require.True(c, ok)
		assert.Equal(c, int64(0), g.Pending)
	}, 20*time.Second, 20*time.Millisecond)

	assert.Len(t, rec.calls(), 5)
	var e Envelope
	require.NoError(t, json.Unmarshal(raw, &e))
	assert.Equal(t, 0, h.count(t, "SELECT count(*) FROM consumed_events WHERE event_id = $1", e.ID))
	assert.Equal(t, 0, h.count(t, "SELECT count(*) FROM consumer_subject_seq WHERE subject_name = 'SO-FAIL'"))
}

func TestConsumerDeliversThaiDataExactly(t *testing.T) {
	const party = "ลูกค้า-001 & <x>"
	h := newHarness(t)
	h.createGroupFromStart(t)
	h.add(t, testEvent(t, "SO-TH", 1, func(d map[string]any) { d["party"] = party }))
	rec := &recorder{}
	h.run(t, h.config(), rec.handle)
	h.waitDrained(t)

	calls := rec.calls()
	require.Len(t, calls, 1)
	assert.Contains(t, string(calls[0].Data), `"party":"`+party+`"`)
	d, err := DecodeData[gen.ErpSalesOrderSubmittedData](calls[0])
	require.NoError(t, err)
	assert.Equal(t, party, d.Party)
}

func TestConsumerCreatesMissingGroupAtLatest(t *testing.T) {
	h := newHarness(t)
	h.add(t, testEvent(t, "SO-OLD", 1, nil)) // before the group exists: never delivered
	rec := &recorder{}
	h.run(t, h.config(), rec.handle)
	require.Eventually(t, func() bool { _, ok := h.groupInfo(t); return ok }, 20*time.Second, 20*time.Millisecond)

	h.add(t, testEvent(t, "SO-NEW", 1, nil))
	h.waitDrained(t)
	assert.Empty(t, rec.seqs("SO-OLD"))
	assert.Equal(t, []int64{1}, rec.seqs("SO-NEW"))
}

func TestPurgeConsumedRemovesOldRows(t *testing.T) {
	pool := storetest.NewPool(t)
	m, _ := platform.NewTestMetrics()
	c := NewConsumer(nil, pool, newValidator(t), m, DefaultConsumerConfig("s", testGroup, "c1"), nil)
	_, err := pool.Exec(t.Context(), `INSERT INTO consumed_events (consumer_group, event_id, consumed_at) VALUES
		($1, 'old', now() - interval '9 days'),
		($1, 'recent', now() - interval '7 days'),
		('agents', 'other-group-old', now() - interval '9 days')`, testGroup)
	require.NoError(t, err)

	n, err := c.PurgeConsumed(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	rows, err := pool.Query(t.Context(), "SELECT event_id FROM consumed_events ORDER BY event_id")
	require.NoError(t, err)
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"other-group-old", "recent"}, ids)
}

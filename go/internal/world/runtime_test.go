package world

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/atipongsena/erp-town/go/internal/events/eventstest"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

type projectionReader struct {
	mu       sync.Mutex
	calls    int
	snapshot erp.ProjectionSnapshot
	err      error
	onRead   func()
}

func (p *projectionReader) ReadProjection(context.Context, erp.Principal, string) (erp.ProjectionSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.onRead != nil {
		p.onRead()
	}
	return p.snapshot, p.err
}

func addWorldEvent(t *testing.T, rdb *redis.Client, stream string, e events.Envelope) string {
	t.Helper()
	raw, err := events.Marshal(e)
	require.NoError(t, err)
	id, err := rdb.XAdd(t.Context(), &redis.XAddArgs{Stream: stream, Values: map[string]any{"event": string(raw)}}).Result()
	require.NoError(t, err)
	return id
}

func TestRestrictedReaderSkipsOtherCompaniesInRecoveryAndLiveDelivery(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	service := NewService(t.Context())
	defer service.Close()
	outside := "Legacy Company Name"
	e := fixture(t, "erp.sales_order.submitted")
	e.Company = &outside
	id := addWorldEvent(t, rdb, stream, e)
	old, err := NewState(outside)
	require.NoError(t, err)
	require.NoError(t, NewSnapshotStore(pool).Save(t.Context(), Snapshot{State: old, StreamID: id, TakenAt: time.Now()}))
	reader := &projectionReader{snapshot: erp.ProjectionSnapshot{Company: "ACME-TH", Facts: []erp.ProjectionFact{}, Sequences: []erp.ProjectionSequence{}}}
	m, _ := platform.NewTestMetrics()
	cfg := DefaultRuntimeConfig(stream)
	cfg.AllowedCompanies = []string{"ACME-TH"}
	runtime, err := NewRuntime(service, rdb, pool, m, reader, erp.Principal{}, cfg)
	require.NoError(t, err)
	require.NoError(t, runtime.Recover(t.Context()))
	require.Equal(t, 0, reader.calls)
	_, ok := runtime.Current(outside)
	require.False(t, ok)
	require.NoError(t, runtime.Handle(events.WithStreamID(t.Context(), id), nil, e))
	require.Equal(t, 0, reader.calls)
	require.ErrorIs(t, runtime.BeforeJoin(t.Context(), outside), erp.ErrForbidden)
	require.NoError(t, runtime.BeforeJoin(t.Context(), "ACME-TH"))
	require.Equal(t, 1, reader.calls)
}

func TestRuntimeReplaysEvenAlreadyConsumedEvents(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	service := NewService(t.Context())
	defer service.Close()
	p, state := setup(t)
	one := fixture(t, "erp.sales_order.submitted")
	one.Seq = 1
	state = apply(t, p, state, one)
	first := addWorldEvent(t, rdb, stream, one)
	store := NewSnapshotStore(pool)
	require.NoError(t, store.Save(t.Context(), Snapshot{State: state, StreamID: first, TakenAt: time.Now()}))
	two := one
	two.Seq = 2
	two.ID = ulid.Make().String()
	two.Data = []byte(`{"status":"Updated","party":"CUST-001","item_count":1,"delivery_date":"2026-10-08"}`)
	second := addWorldEvent(t, rdb, stream, two)
	_, err := pool.Exec(t.Context(), "INSERT INTO consumed_events(consumer_group,event_id) VALUES('world',$1)", two.ID)
	require.NoError(t, err)
	reader := &projectionReader{err: errors.New("valid checkpoint must not rebuild")}
	m, _ := platform.NewTestMetrics()
	runtime, err := NewRuntime(service, rdb, pool, m, reader, erp.Principal{}, DefaultRuntimeConfig(stream))
	require.NoError(t, err)
	require.NoError(t, runtime.Recover(t.Context()))
	current, ok := runtime.Current(state.Company())
	require.True(t, ok)
	require.Equal(t, int64(2), current.LastSeq(two.Subject))
	assert.Contains(t, string(current.Entities()[0].Data), "Updated")
	assert.Equal(t, 0, reader.calls)
	require.NoError(t, runtime.Checkpoint(t.Context()))
	saved, ok, err := store.Load(t.Context(), state.Company())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, second, saved.StreamID)
}

func TestRuntimeRebuildCapturesEventsArrivingDuringERPRead(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	service := NewService(t.Context())
	defer service.Close()
	e := fixture(t, "erp.sales_order.submitted")
	e.Seq = 1
	addWorldEvent(t, rdb, stream, e)
	ref := erp.DocRef{DocType: erp.DocSalesOrder, Name: e.Subject.Name}
	reader := &projectionReader{snapshot: erp.ProjectionSnapshot{Company: "ACME-TH", Facts: []erp.ProjectionFact{{Ref: ref, Kind: "document", Status: "To Deliver"}}, Sequences: []erp.ProjectionSequence{{Ref: ref, Seq: 1}}}}
	reader.onRead = func() {
		groups, err := rdb.XInfoGroups(t.Context(), stream).Result()
		require.NoError(t, err)
		require.Len(t, groups, 1, "live group exists before ERP recovery")
		newer := fixture(t, "workflow.approval.requested")
		newer.Subject = e.Subject
		newer.Seq = 2
		addWorldEvent(t, rdb, stream, newer)
	}
	m, _ := platform.NewTestMetrics()
	runtime, err := NewRuntime(service, rdb, pool, m, reader, erp.Principal{}, DefaultRuntimeConfig(stream))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	require.NoError(t, runtime.BeforeJoin(t.Context(), "ACME-TH"))
	require.EventuallyWithT(t, func(a *assert.CollectT) {
		state, ok := runtime.Current("ACME-TH")
		require.True(a, ok)
		assert.Len(a, state.Entities(), 2)
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestRuntimeDoesNotCheckpointPastPendingEntry(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	service := NewService(t.Context())
	defer service.Close()
	p, state := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	e.Seq = 1
	state = apply(t, p, state, e)
	first := addWorldEvent(t, rdb, stream, e)
	store := NewSnapshotStore(pool)
	require.NoError(t, store.Save(t.Context(), Snapshot{State: state, StreamID: first, TakenAt: time.Now()}))
	m, _ := platform.NewTestMetrics()
	runtime, err := NewRuntime(service, rdb, pool, m, &projectionReader{}, erp.Principal{}, DefaultRuntimeConfig(stream))
	require.NoError(t, err)
	require.NoError(t, runtime.Recover(t.Context()))
	old := e
	old.ID = ulid.Make().String()
	old.Seq = 2
	pending := addWorldEvent(t, rdb, stream, old)
	newer := e
	newer.ID = ulid.Make().String()
	newer.Seq = 3
	latest := addWorldEvent(t, rdb, stream, newer)
	_, err = rdb.XReadGroup(t.Context(), &redis.XReadGroupArgs{Group: "world", Consumer: "test", Streams: []string{stream, ">"}, Count: 10}).Result()
	require.NoError(t, err)
	require.NoError(t, runtime.Handle(events.WithStreamID(t.Context(), latest), nil, newer))
	require.NoError(t, rdb.XAck(t.Context(), stream, "world", latest).Err())
	require.NoError(t, runtime.Checkpoint(t.Context()))
	saved, _, err := store.Load(t.Context(), "ACME-TH")
	require.NoError(t, err)
	assert.Equal(t, first, saved.StreamID)
	require.NoError(t, rdb.XAck(t.Context(), stream, "world", pending).Err())
	require.NoError(t, runtime.Checkpoint(t.Context()))
	saved, _, err = store.Load(t.Context(), "ACME-TH")
	require.NoError(t, err)
	assert.Equal(t, latest, saved.StreamID)
}

func TestRuntimeTrimRebuildKeepsVersionMonotonic(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	service := NewService(t.Context())
	defer service.Close()
	p, state := setup(t)
	e := fixture(t, "erp.sales_invoice.overdue")
	state = apply(t, p, state, e)
	cursor := addWorldEvent(t, rdb, stream, e)
	store := NewSnapshotStore(pool)
	require.NoError(t, store.Save(t.Context(), Snapshot{State: state, StreamID: cursor, TakenAt: time.Now()}))
	require.NoError(t, rdb.XDel(t.Context(), stream, cursor).Err())
	ref := erp.DocRef{DocType: erp.DocSalesInvoice, Name: e.Subject.Name}
	reader := &projectionReader{snapshot: erp.ProjectionSnapshot{Company: "ACME-TH", Facts: []erp.ProjectionFact{}, Sequences: []erp.ProjectionSequence{{Ref: ref, Seq: e.Seq}}}}
	m, _ := platform.NewTestMetrics()
	runtime, err := NewRuntime(service, rdb, pool, m, reader, erp.Principal{}, DefaultRuntimeConfig(stream))
	require.NoError(t, err)
	require.NoError(t, runtime.Recover(t.Context()))
	current, ok := runtime.Current("ACME-TH")
	require.True(t, ok)
	assert.Empty(t, current.Entities())
	assert.Greater(t, current.Version(), state.Version())
	assert.Equal(t, 1, reader.calls)
}

type trimDuringReplay struct {
	rdb          *redis.Client
	stream, keep string
	fired        atomic.Bool
}

func (h *trimDuringReplay) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *trimDuringReplay) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *trimDuringReplay) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "xrange" && len(cmd.Args()) > 2 && cmd.Args()[2] == "(0-0" && h.fired.CompareAndSwap(false, true) {
			if err := h.rdb.XTrimMinID(ctx, h.stream, h.keep).Err(); err != nil {
				return err
			}
		}
		return next(ctx, cmd)
	}
}

func TestRuntimeTrimDuringReplayRebuildsLostPrefix(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	service := NewService(t.Context())
	defer service.Close()
	p, state := setup(t)
	one := fixture(t, "erp.sales_order.submitted")
	one.Seq = 1
	one.Subject.Name = "SO-OLD"
	state = apply(t, p, state, one)
	cursor := addWorldEvent(t, rdb, stream, one)
	require.NoError(t, NewSnapshotStore(pool).Save(t.Context(), Snapshot{State: state, StreamID: cursor, TakenAt: time.Now()}))
	two := one
	two.Subject.Name = "SO-MISSING"
	two.ID = ulid.Make().String()
	addWorldEvent(t, rdb, stream, two)
	three := one
	three.Subject.Name = "SO-LAST"
	three.ID = ulid.Make().String()
	last := addWorldEvent(t, rdb, stream, three)
	reader := &projectionReader{snapshot: erp.ProjectionSnapshot{Company: "ACME-TH", Facts: []erp.ProjectionFact{
		{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: two.Subject.Name}, Kind: "document", Status: "To Deliver"},
		{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: three.Subject.Name}, Kind: "document", Status: "To Deliver"},
	}, Sequences: []erp.ProjectionSequence{{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: one.Subject.Name}, Seq: 1}, {Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: two.Subject.Name}, Seq: 1}, {Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: three.Subject.Name}, Seq: 1}}}}
	rdb.AddHook(&trimDuringReplay{rdb: rdb, stream: stream, keep: last})
	m, _ := platform.NewTestMetrics()
	runtime, err := NewRuntime(service, rdb, pool, m, reader, erp.Principal{}, DefaultRuntimeConfig(stream))
	require.NoError(t, err)
	require.NoError(t, runtime.Recover(t.Context()))
	current, ok := runtime.Current("ACME-TH")
	require.True(t, ok)
	names := []string{}
	for _, entity := range current.Entities() {
		names = append(names, entity.Key.Subject.Name)
	}
	assert.ElementsMatch(t, []string{"SO-MISSING", "SO-LAST"}, names)
	assert.Equal(t, 1, reader.calls)
}

func TestRuntimeRawReplayDedupesIDWithoutUsingDurableLedger(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	service := NewService(t.Context())
	defer service.Close()
	p, state := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	e.Seq = 1
	state = apply(t, p, state, e)
	cursor := addWorldEvent(t, rdb, stream, e)
	require.NoError(t, NewSnapshotStore(pool).Save(t.Context(), Snapshot{State: state, StreamID: cursor, TakenAt: time.Now()}))
	duplicate := e
	duplicate.Seq = 2 // producer violation: same ID must never carry a changed envelope
	duplicate.Data = []byte(`{"status":"Updated","party":"CUST-001","item_count":1,"delivery_date":"2026-10-08"}`)
	addWorldEvent(t, rdb, stream, duplicate)
	m, _ := platform.NewTestMetrics()
	runtime, err := NewRuntime(service, rdb, pool, m, &projectionReader{}, erp.Principal{}, DefaultRuntimeConfig(stream))
	require.NoError(t, err)
	require.NoError(t, runtime.Recover(t.Context()))
	current, ok := runtime.Current(state.Company())
	require.True(t, ok)
	assert.Equal(t, state, current)
}

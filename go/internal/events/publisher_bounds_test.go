package events

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/events/gen"
)

// lostReplyConn lets Redis execute XADD, then discards the successful reply.
// Retrying inside go-redis would append duplicates while counting one outbox attempt.
type lostReplyConn struct {
	net.Conn
	added bool
}

func (c *lostReplyConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if bytes.Contains(bytes.ToLower(b[:n]), []byte("\r\nxadd\r\n")) {
		c.added = true
	}
	return n, err
}

func (c *lostReplyConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if c.added && n > 0 {
		return 0, io.EOF
	}
	return n, err
}

func TestPublishLostReplyIsOneRedisAttempt(t *testing.T) {
	h := newPubHarness(t)
	opts := *h.rdb.Options()
	opts.Protocol = 2
	opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &lostReplyConn{Conn: conn}, nil
	}
	rdb := NewPublisherRedisClient(&opts)
	t.Cleanup(func() { _ = rdb.Close() })
	id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("AMBIGUOUS", gen.SystemErrorData{Code: "TEST", Message: "m"}))
	n, err := h.publisher(rdb, testNow).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, 1, loadRow(t, h.pool, id).Attempts)
	length, err := h.rdb.XLen(t.Context(), h.stream).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), length, "one recorded failure must not hide Redis retries")

	// The durable retry is still allowed; the identical event id lets consumers dedupe.
	n, err = h.publisher(h.rdb, testNow.Add(2*time.Second)).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	msgs, err := h.rdb.XRange(t.Context(), h.stream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, msgs[0].Values, msgs[1].Values)
}

type waitForPublishDeadline struct {
	deadlines atomic.Int32
}

func (*waitForPublishDeadline) DialHook(next redis.DialHook) redis.DialHook { return next }
func (*waitForPublishDeadline) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *waitForPublishDeadline) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() != "xadd" {
			return next(ctx, cmd)
		}
		if _, ok := ctx.Deadline(); !ok {
			return io.ErrUnexpectedEOF
		}
		h.deadlines.Add(1)
		<-ctx.Done()
		return ctx.Err()
	}
}

func TestPublishHasPerXAddDeadline(t *testing.T) {
	h := newPubHarness(t)
	hook := &waitForPublishDeadline{}
	h.rdb.AddHook(hook)
	id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("TIMEOUT", gen.SystemErrorData{Code: "TEST", Message: "m"}))
	other := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("OTHER", gen.SystemErrorData{Code: "TEST", Message: "m"}))
	start := time.Now()
	n, err := h.publisher(h.rdb, testNow).PublishDue(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, int32(1), hook.deadlines.Load(), "a broken connection skips XADD for the rest of the batch")
	assert.Less(t, time.Since(start), 7*time.Second)
	assert.Equal(t, 1, loadRow(t, h.pool, id).Attempts)
	assert.Equal(t, 1, loadRow(t, h.pool, other).Attempts)
}

func TestRunPurgesOnlyExpiredPublishedRows(t *testing.T) {
	h := newPubHarness(t)
	cutoff := testNow.Add(-400 * 24 * time.Hour)
	for _, row := range []struct {
		id, status string
		published  *time.Time
	}{
		{"expired", StatusPublished, new(cutoff.Add(-time.Microsecond))},
		{"boundary", StatusPublished, &cutoff},
		{"recent", StatusPublished, &testNow},
		{"missing-time", StatusPublished, nil},
		{"pending", StatusPending, nil},
		{"failed", StatusFailed, new(cutoff.Add(-time.Hour))},
		{"invalid", StatusInvalid, new(cutoff.Add(-time.Hour))},
	} {
		_, err := h.pool.Exec(t.Context(), `INSERT INTO town_outbox
			(id, type, subject_doctype, subject_name, payload, status, created_at, published_at, next_attempt_at)
			VALUES ($1, 'system.error', 'Town Event Outbox', $1, '{}', $2, $3, $4, $5)`,
			row.id, row.status, cutoff.Add(-time.Hour), row.published, testNow.Add(time.Hour))
		require.NoError(t, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- h.publisher(h.rdb, testNow).Run(ctx, 20*time.Millisecond) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool {
		var count int
		err := h.pool.QueryRow(t.Context(), "SELECT count(*) FROM town_outbox WHERE id = 'expired'").Scan(&count)
		return err == nil && count == 0
	}, 2*time.Second, 20*time.Millisecond)
	var ids []string
	for _, r := range loadRows(t, h.pool) {
		ids = append(ids, r.ID)
	}
	assert.ElementsMatch(t, []string{"boundary", "recent", "missing-time", "pending", "failed", "invalid"}, ids)
}

// stalledReplyConn receives the successful XADD response, then waits for more
// bytes. The actual TCP socket's read deadline must release the publisher lock.
type stalledReplyConn struct{ lostReplyConn }

func (c *stalledReplyConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if c.added && n > 0 {
		return c.Conn.Read(b)
	}
	return n, err
}

func TestPublishSocketDeadlineAndParentCancellation(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		t.Run(strconv.FormatBool(parentDeadline), func(t *testing.T) {
			h := newPubHarness(t)
			opts := *h.rdb.Options()
			opts.Protocol = 2
			opts.ReadTimeout = 30 * time.Second
			opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				return &stalledReplyConn{lostReplyConn{Conn: conn}}, nil
			}
			rdb := NewPublisherRedisClient(&opts)
			t.Cleanup(func() { _ = rdb.Close() })
			require.NoError(t, rdb.Ping(t.Context()).Err())
			id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("SOCKET", gen.SystemErrorData{Code: "TEST", Message: "m"}))
			cfg := h.config(testNow)
			cfg.PublishTimeout = 100 * time.Millisecond
			ctx := t.Context()
			if parentDeadline {
				cfg.PublishTimeout = 5 * time.Second
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			start := time.Now()
			n, err := NewPublisher(h.pool, rdb, h.v, h.m, cfg).PublishDue(ctx)
			require.NoError(t, err)
			assert.Zero(t, n)
			assert.Less(t, time.Since(start), time.Second, "socket deadline releases the row lock")
			r := loadRow(t, h.pool, id)
			assert.Equal(t, StatusPending, r.Status)
			if parentDeadline {
				assert.Zero(t, r.Attempts, "shutdown is not a failed outbox attempt")
				assert.Nil(t, r.NextAttemptAt)
			} else {
				assert.Equal(t, 1, r.Attempts)
				require.NotNil(t, r.NextAttemptAt)
				assert.True(t, testNow.Add(2*time.Second).Equal(*r.NextAttemptAt))
			}
			inTx(t, h.pool, func(tx pgx.Tx) {
				_, err := tx.Exec(t.Context(), "SELECT id FROM town_outbox WHERE id = $1 FOR UPDATE NOWAIT", id)
				require.NoError(t, err, "the completed call releases its lock")
			})
		})
	}
}

func TestPurgePublishedIsBoundedSkipsLocksAndPreservesSeq(t *testing.T) {
	h := newPubHarness(t)
	var ids []string
	in := systemErrorInput("RETAIN-SEQ", gen.SystemErrorData{Code: "TEST", Message: "m"})
	for range 4 {
		ids = append(ids, emitCommitted(t, h.pool, h.v, testNow, in))
	}
	_, err := h.pool.Exec(t.Context(), "UPDATE town_outbox SET status = 'Published', published_at = $1", testNow.Add(-2*24*time.Hour))
	require.NoError(t, err)
	cfg := h.config(testNow)
	cfg.OutboxRetentionDays, cfg.BatchSize = 1, 2
	p := NewPublisher(h.pool, h.rdb, h.v, h.m, cfg)
	inTx(t, h.pool, func(tx pgx.Tx) {
		_, err := tx.Exec(t.Context(), "SELECT id FROM town_outbox WHERE id = $1 FOR UPDATE", ids[0])
		require.NoError(t, err)
		n, err := p.PurgePublished(t.Context())
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		assert.Len(t, loadRows(t, h.pool), 2)
		assert.Equal(t, StatusPublished, loadRow(t, h.pool, ids[0]).Status)
		n, err = p.PurgePublished(t.Context())
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
	})
	n, err := p.PurgePublished(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	assert.Empty(t, loadRows(t, h.pool))
	next := emitCommitted(t, h.pool, h.v, testNow, in)
	assert.Equal(t, int64(5), decodeStored(t, h.v, loadRow(t, h.pool, next).Payload).Seq)
}

func TestRunDrainsCleanupBacklogAndRepeatsDaily(t *testing.T) {
	h := newPubHarness(t)
	var clock atomic.Int64
	clock.Store(testNow.UnixNano())
	cfg := h.config(testNow)
	cfg.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	cfg.OutboxRetentionDays, cfg.BatchSize = 1, 2
	for i := range 5 {
		_, err := h.pool.Exec(t.Context(), `INSERT INTO town_outbox
			(id, type, subject_doctype, subject_name, payload, status, published_at)
			VALUES ($1, 'system.error', 'Town Event Outbox', $1, '{}', 'Published', $2)`,
			strconv.Itoa(i), testNow.Add(-2*24*time.Hour))
		require.NoError(t, err)
	}
	id := emitCommitted(t, h.pool, h.v, testNow, systemErrorInput("NEW", gen.SystemErrorData{Code: "TEST", Message: "m"}))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- NewPublisher(h.pool, h.rdb, h.v, h.m, cfg).Run(ctx, 20*time.Millisecond) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool {
		rows := loadRows(t, h.pool)
		return len(rows) == 1 && rows[0].ID == id && rows[0].Status == StatusPublished
	}, 3*time.Second, 20*time.Millisecond)
	clock.Store(testNow.Add(24*time.Hour + time.Microsecond).UnixNano())
	require.Eventually(t, func() bool { return len(loadRows(t, h.pool)) == 0 }, 3*time.Second, 20*time.Millisecond)
}

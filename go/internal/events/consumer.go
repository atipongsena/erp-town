package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/sqlc"
)

// Handler applies one event. tx is the consumer's transaction that also records the
// event id (dedupe) and the subject's new seq: Postgres writes the handler makes through
// tx commit together with them, so they happen exactly once per group. An error rolls
// the whole transaction back and leaves the stream entry pending; it is delivered again
// after ClaimIdle (the handler sees it at most MaxDeliveries times). Side effects
// outside Postgres are at-least-once. The handler must not commit or roll back tx.
// A panic is treated as a failed delivery: its transaction is rolled back and the
// entry stays pending for retry, subject to MaxDeliveries.
type Handler func(ctx context.Context, tx pgx.Tx, e Envelope) error

// ConsumerConfig configures one member of a Redis Streams consumer group.
type ConsumerConfig struct {
	Stream, Group, Name string
	// Block is how long one XREADGROUP waits for new entries; it also bounds how long
	// Run takes to notice a cancelled context.
	Block time.Duration
	// ClaimIdle is how long an entry stays pending before XAUTOCLAIM takes it over.
	ClaimIdle time.Duration
	// MaxDeliveries is how many times the handler may see an entry; an entry claimed
	// with a higher delivery count is acked and dropped without calling the handler.
	MaxDeliveries int64
	// DedupeWindow is how long consumed event ids are remembered.
	DedupeWindow time.Duration
}

// DefaultConsumerConfig returns the town defaults: Block 5 s, ClaimIdle 60 s,
// MaxDeliveries 5, DedupeWindow 8 days (longer than the stream's 7-day retention).
func DefaultConsumerConfig(stream, group, name string) ConsumerConfig {
	return ConsumerConfig{
		Stream: stream, Group: group, Name: name,
		Block:         5 * time.Second,
		ClaimIdle:     60 * time.Second,
		MaxDeliveries: 5,
		DedupeWindow:  8 * 24 * time.Hour,
	}
}

const (
	eventField    = "event" // stream entry field holding the UTF-8 JSON envelope
	readCount     = 10
	purgeInterval = time.Hour
	retryDelay    = time.Second
	cleanupTime   = 5 * time.Second
	pingTimeout   = 5 * time.Second
	dbRetryMin    = 200 * time.Millisecond
	dbRetryMax    = 5 * time.Second
)

// Consumer reads town events from a stream through a consumer group and hands each
// valid, new, in-order event to its Handler. The Handler's Postgres effects (through
// its tx) commit exactly once per group; the Handler itself runs at most
// MaxDeliveries times per event.
type Consumer struct {
	rdb  *redis.Client
	pool *pgxpool.Pool
	v    *Validator
	m    *platform.Metrics
	cfg  ConsumerConfig
	h    Handler
	log  *slog.Logger
}

// NewConsumer creates a consumer; nothing is read until Run.
func NewConsumer(rdb *redis.Client, pool *pgxpool.Pool, v *Validator, m *platform.Metrics, cfg ConsumerConfig, h Handler) *Consumer {
	return &Consumer{
		rdb: rdb, pool: pool, v: v, m: m, cfg: cfg, h: h,
		log: slog.Default().With("stream", cfg.Stream, "group", cfg.Group, "consumer", cfg.Name),
	}
}

// Run creates the group (MKSTREAM, from $) if it is missing, then reclaims idle pending
// entries and reads new ones until ctx is cancelled, when it returns nil. It purges
// expired dedupe rows at start and hourly. Redis errors are logged and retried after
// 1 s; while Postgres is unreachable the consumer pauses on the current entry (see
// process). Only failing to create the group at start is returned.
func (c *Consumer) Run(ctx context.Context) error {
	if err := c.ensureGroup(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	c.purge(ctx)
	purge := time.NewTicker(purgeInterval)
	defer purge.Stop()
	for ctx.Err() == nil {
		select {
		case <-purge.C:
			c.purge(ctx)
		default:
		}
		if err := c.step(ctx); err != nil && ctx.Err() == nil {
			c.log.Error("consumer: read failed", "err", err)
			if redis.HasErrorPrefix(err, "NOGROUP") {
				if err := c.ensureGroup(ctx); err != nil && ctx.Err() == nil {
					c.log.Error("consumer: recreate group failed", "err", err)
				}
			}
			sleep(ctx, retryDelay)
		}
	}
	return nil
}

// PurgeConsumed deletes this group's consumed_events rows older than DedupeWindow and
// returns how many were deleted.
func (c *Consumer) PurgeConsumed(ctx context.Context) (int64, error) {
	n, err := sqlc.New(c.pool).PurgeConsumed(ctx, sqlc.PurgeConsumedParams{
		ConsumerGroup: c.cfg.Group,
		WindowSeconds: c.cfg.DedupeWindow.Seconds(),
	})
	if err != nil {
		return 0, fmt.Errorf("consumer: purge consumed events: %w", err)
	}
	return n, nil
}

func (c *Consumer) purge(ctx context.Context) {
	n, err := c.PurgeConsumed(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.log.Error("consumer: purge failed", "err", err)
		}
		return
	}
	c.log.Debug("consumer: purged consumed events", "rows", n)
}

func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.rdb.XGroupCreateMkStream(ctx, c.cfg.Stream, c.cfg.Group, "$").Err()
	if err != nil && !redis.HasErrorPrefix(err, "BUSYGROUP") {
		return fmt.Errorf("consumer: create group %s on %s: %w", c.cfg.Group, c.cfg.Stream, err)
	}
	return nil
}

// step reclaims idle pending entries, then reads and processes one batch of new ones.
func (c *Consumer) step(ctx context.Context) error {
	if err := c.reclaim(ctx); err != nil {
		return err
	}
	streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.cfg.Group,
		Consumer: c.cfg.Name,
		Streams:  []string{c.cfg.Stream, ">"},
		Count:    readCount,
		Block:    c.cfg.Block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("xreadgroup: %w", err)
	}
	for _, s := range streams {
		for _, msg := range s.Messages {
			if ctx.Err() != nil {
				return nil
			}
			c.process(ctx, msg)
		}
	}
	return nil
}

// reclaim takes over every entry of the group pending for at least ClaimIdle (its own
// included) and processes it, or drops it once it has been delivered too often.
func (c *Consumer) reclaim(ctx context.Context) error {
	start := "0-0"
	for {
		msgs, next, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   c.cfg.Stream,
			Group:    c.cfg.Group,
			Consumer: c.cfg.Name,
			MinIdle:  c.cfg.ClaimIdle,
			Start:    start,
			Count:    readCount,
		}).Result()
		if err != nil {
			return fmt.Errorf("xautoclaim: %w", err)
		}
		for _, msg := range msgs {
			if ctx.Err() != nil {
				return nil
			}
			n, err := c.deliveries(ctx, msg.ID)
			if err != nil {
				return err
			}
			switch {
			case n == 0: // acked by another consumer in the meantime
			case n > c.cfg.MaxDeliveries:
				c.drop(ctx, msg, n)
			default:
				c.process(ctx, msg)
			}
		}
		if next == "0-0" || next == "" {
			return nil
		}
		start = next
	}
}

// deliveries returns the entry's delivery count (including the claim that just took
// it), or 0 when it is no longer pending for this consumer.
func (c *Consumer) deliveries(ctx context.Context, id string) (int64, error) {
	p, err := c.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream:   c.cfg.Stream,
		Group:    c.cfg.Group,
		Start:    id,
		End:      id,
		Count:    1,
		Consumer: c.cfg.Name,
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("xpending %s: %w", id, err)
	}
	if len(p) == 0 {
		return 0, nil
	}
	return p[0].RetryCount, nil
}

func (c *Consumer) drop(ctx context.Context, msg redis.XMessage, deliveries int64) {
	if !c.ack(ctx, msg.ID) {
		return
	}
	c.m.EventsDropped.Add(ctx, 1)
	c.log.Warn("consumer: entry dropped after max deliveries",
		"entry", msg.ID, "event_id", eventID(msg), "deliveries", deliveries)
}

// process handles one entry: invalid entries are acked and counted; otherwise the entry
// is acked only after apply committed. While Postgres is unreachable process blocks
// (retrying the same entry with backoff), so the consumer neither reads nor reclaims
// and an outage never uses up deliveries; only handler failures (and statement errors
// while the database answers) leave the entry pending toward MaxDeliveries.
func (c *Consumer) process(ctx context.Context, msg redis.XMessage) {
	raw, ok := msg.Values[eventField].(string)
	if !ok {
		c.invalid(ctx, msg, fmt.Errorf("entry has no %q field", eventField))
		return
	}
	e, err := Decode(c.v, []byte(raw))
	if err != nil {
		c.invalid(ctx, msg, err)
		return
	}
	for {
		err := c.apply(WithStreamID(ctx, msg.ID), e)
		if err == nil {
			c.ack(ctx, msg.ID)
			return
		}
		if ctx.Err() != nil {
			return
		}
		var he *handlerError
		if errors.As(err, &he) {
			c.log.Warn("consumer: handler failed, entry left pending",
				"entry", msg.ID, "event_id", e.ID, "type", e.Type, "err", err)
			return
		}
		if c.dbReachable(ctx) {
			// The database answers but our statement failed: treat it like a handler
			// failure (counts toward MaxDeliveries) so one bad entry cannot stall the group.
			c.log.Error("consumer: event not applied, entry left pending",
				"entry", msg.ID, "event_id", e.ID, "type", e.Type, "err", err)
			return
		}
		// Outage: hold this entry (and the rest of the batch) without reading or
		// reclaiming anything, so no delivery is used up, and retry once Postgres is back.
		c.log.Error("consumer: database unavailable, pausing", "entry", msg.ID, "event_id", e.ID, "err", err)
		if !c.waitForDB(ctx) {
			return
		}
		c.log.Info("consumer: database available again, resuming", "entry", msg.ID)
	}
}

// handlerError marks a failure returned by the Handler, as opposed to the consumer's
// own Postgres work.
type handlerError struct{ err error }

func (e *handlerError) Error() string { return "handler: " + e.err.Error() }
func (e *handlerError) Unwrap() error { return e.err }

func (c *Consumer) dbReachable(ctx context.Context) bool {
	pctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return c.pool.Ping(pctx) == nil
}

// waitForDB pings Postgres with exponential backoff (200 ms doubling, capped at 5 s)
// until it answers (true) or ctx is cancelled (false).
func (c *Consumer) waitForDB(ctx context.Context) bool {
	delay := dbRetryMin
	for {
		sleep(ctx, delay)
		if ctx.Err() != nil {
			return false
		}
		if c.dbReachable(ctx) {
			return true
		}
		delay = min(2*delay, dbRetryMax)
	}
}

func (c *Consumer) invalid(ctx context.Context, msg redis.XMessage, err error) {
	if !c.ack(ctx, msg.ID) {
		return
	}
	c.m.EventsInvalid.Add(ctx, 1)
	c.log.Warn("consumer: invalid entry acked", "entry", msg.ID, "event_id", eventID(msg), "err", err)
}

// apply dedupes by event id, gates by per-subject seq and runs the handler, all in one
// transaction. A nil return means the entry is done (applied, duplicate or stale).
func (c *Consumer) apply(ctx context.Context, e Envelope) (err error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTime)
			defer cancel()
			_ = tx.Rollback(rctx)
		}
	}()
	q := sqlc.New(tx)

	n, err := q.InsertConsumed(ctx, sqlc.InsertConsumedParams{ConsumerGroup: c.cfg.Group, EventID: e.ID})
	if err != nil {
		return fmt.Errorf("record consumed: %w", err)
	}
	if n == 0 { // seen before: a redelivery or the same id published twice
		return commit(ctx, tx)
	}

	subject := sqlc.EnsureSubjectSeqParams{ConsumerGroup: c.cfg.Group, SubjectDoctype: e.Subject.DocType, SubjectName: e.Subject.Name}
	if err := q.EnsureSubjectSeq(ctx, subject); err != nil {
		return fmt.Errorf("ensure subject seq: %w", err)
	}
	last, err := q.LockSubjectSeq(ctx, sqlc.LockSubjectSeqParams(subject))
	if err != nil {
		return fmt.Errorf("lock subject seq: %w", err)
	}
	if e.Seq <= last { // stale; never wait for a missing seq either
		if err := commit(ctx, tx); err != nil {
			return err
		}
		c.m.EventsStale.Add(ctx, 1)
		c.log.Warn("consumer: stale seq skipped", "event_id", e.ID, "type", e.Type,
			"group", c.cfg.Group, "subject_doctype", e.Subject.DocType, "subject_name", e.Subject.Name,
			"seq", e.Seq, "last_seq", last)
		return nil
	}

	if err := c.handle(ctx, tx, e); err != nil {
		return &handlerError{err: err}
	}
	// The handler succeeded: finish even if ctx is cancelled now, so a shutdown does not
	// make the handler run a second time on redelivery.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTime)
	defer cancel()
	if err := q.SetSubjectSeq(fctx, sqlc.SetSubjectSeqParams{
		ConsumerGroup: c.cfg.Group, SubjectDoctype: e.Subject.DocType, SubjectName: e.Subject.Name, LastSeq: e.Seq,
	}); err != nil {
		return fmt.Errorf("set subject seq: %w", err)
	}
	return commit(fctx, tx)
}

// handle confines recovery to the application handler, leaving consumer defects
// visible. Returning an error lets apply roll back before process retries later.
func (c *Consumer) handle(ctx context.Context, tx pgx.Tx, e Envelope) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return c.h(ctx, tx, e)
}

func commit(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ack acknowledges the entry. It survives cancellation of ctx so that work committed
// just before shutdown is not redelivered needlessly (a redelivery would be deduped).
func (c *Consumer) ack(ctx context.Context, id string) bool {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTime)
	defer cancel()
	if err := c.rdb.XAck(actx, c.cfg.Stream, c.cfg.Group, id).Err(); err != nil {
		c.log.Error("consumer: xack failed", "entry", id, "err", err)
		return false
	}
	return true
}

// eventID extracts the envelope id from an entry for logging, or "" if it has none.
func eventID(msg redis.XMessage) string {
	raw, _ := msg.Values[eventField].(string)
	var probe struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(raw), &probe)
	return probe.ID
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

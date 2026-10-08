package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/sqlc"
)

// PublisherConfig configures the outbox publisher.
type PublisherConfig struct {
	// Stream is the town stream entries are added to.
	Stream string
	// RetentionDays bounds the stream: every XADD trims entries older than this
	// (MINID, approximate).
	RetentionDays int
	// MaxAttempts is how many failed publishes turn a row Failed.
	MaxAttempts int
	// BatchSize is the most rows one PublishDue locks and publishes.
	BatchSize int
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// DefaultPublisherConfig returns the town defaults: 7-day retention, 10 attempts,
// batches of 500, the real clock.
func DefaultPublisherConfig(stream string) PublisherConfig {
	return PublisherConfig{Stream: stream, RetentionDays: 7, MaxAttempts: 10, BatchSize: 500, Now: time.Now}
}

const maxBackoff = 300 * time.Second

// Publisher moves Pending town_outbox rows to the stream with the Frappe outbox's
// rules, so consumers cannot tell the two producers apart.
type Publisher struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
	v    *Validator
	m    *platform.Metrics
	cfg  PublisherConfig
	log  *slog.Logger
}

// NewPublisher creates a publisher; zero config fields take DefaultPublisherConfig's values.
func NewPublisher(pool *pgxpool.Pool, rdb *redis.Client, v *Validator, m *platform.Metrics, cfg PublisherConfig) *Publisher {
	def := DefaultPublisherConfig(cfg.Stream)
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = def.RetentionDays
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = def.MaxAttempts
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = def.BatchSize
	}
	if cfg.Now == nil {
		cfg.Now = def.Now
	}
	return &Publisher{
		pool: pool, rdb: rdb, v: v, m: m, cfg: cfg,
		log: slog.Default().With("stream", cfg.Stream),
	}
}

// PublishDue publishes due Pending rows and returns how many were published. In one
// transaction it locks up to BatchSize rows that are Pending and due (next_attempt_at
// unset or not after now), oldest first, skipping rows another publisher holds
// (FOR UPDATE SKIP LOCKED); XADDs each payload as field "event" while the rows are
// locked, trimming the stream to RetentionDays (MINID ~); records each result and
// commits.
//
// A successful XADD marks the row Published with its stream id. A failed one
// increments attempts, stores the error and retries after min(2^attempts, 300) s,
// counting town_outbox_publish_failures_total; at MaxAttempts the row turns Failed and,
// unless it is a system.error itself, a system.error OUTBOX_PUBLISH_FAILED about it is
// emitted in the same transaction. After a connection-level Redis error the rest of the
// batch takes the failure path without trying Redis again. Per-row failures are not
// errors; the returned error is a Postgres failure, after which entries already added
// may be added again (consumers dedupe by id).
//
// When ctx is cancelled no further row is tried; the rows already handled are still
// recorded and committed, the rest stay untouched.
func (p *Publisher) PublishDue(ctx context.Context) (published int, err error) {
	now := p.cfg.Now()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("publisher: begin: %w", err)
	}
	defer func() {
		if err != nil {
			rctx, cancel := detached(ctx)
			defer cancel()
			_ = tx.Rollback(rctx)
		}
	}()
	q := sqlc.New(tx)
	rows, err := q.LockDueOutbox(ctx, sqlc.LockDueOutboxParams{Now: timestamptz(now), BatchSize: int32(min(p.cfg.BatchSize, 1<<30))})
	if err != nil {
		return 0, fmt.Errorf("publisher: lock due rows: %w", err)
	}

	var broken error // connection-level Redis error: skip XADD for the rest of the batch
	for _, r := range rows {
		if ctx.Err() != nil {
			break
		}
		streamID, xerr := "", broken
		if xerr == nil {
			streamID, xerr = p.xadd(ctx, now, r.Payload)
			if xerr != nil && ctx.Err() != nil {
				break // shutting down: not a publish failure
			}
			var reply redis.Error
			if xerr != nil && !errors.As(xerr, &reply) {
				broken = xerr
			}
		}
		// From here on Redis may hold the entry: record the outcome even if ctx is cancelled.
		if xerr != nil {
			if err := p.recordFailure(ctx, tx, now, r, xerr); err != nil {
				return published, err
			}
			continue
		}
		if err := p.markPublished(ctx, q, now, r.ID, streamID); err != nil {
			return published, err
		}
		published++
	}

	cctx, cancel := detached(ctx)
	defer cancel()
	if err := tx.Commit(cctx); err != nil {
		return 0, fmt.Errorf("publisher: commit: %w", err)
	}
	return published, nil
}

// Run calls PublishDue every interval until ctx is cancelled, then returns nil; a full
// batch is followed by the next one right away. Errors are logged and retried on the
// next tick.
func (p *Publisher) Run(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("publisher: interval must be positive, got %v", every)
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		n, err := p.PublishDue(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			p.log.Error("publisher: publish failed", "err", err)
		} else if n == p.cfg.BatchSize {
			continue // backlog: take the next batch now
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func (p *Publisher) xadd(ctx context.Context, now time.Time, payload string) (string, error) {
	minID := max(now.UnixMilli()-int64(p.cfg.RetentionDays)*24*time.Hour.Milliseconds(), 0)
	id, err := p.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: p.cfg.Stream,
		MinID:  strconv.FormatInt(minID, 10) + "-0",
		Approx: true,
		ID:     "*",
		Values: []any{eventField, payload},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("xadd: %w", err)
	}
	return id, nil
}

func (p *Publisher) markPublished(ctx context.Context, q *sqlc.Queries, now time.Time, id, streamID string) error {
	sctx, cancel := detached(ctx)
	defer cancel()
	if err := q.MarkOutboxPublished(sctx, sqlc.MarkOutboxPublishedParams{
		ID: id, StreamID: pgtype.Text{String: streamID, Valid: true}, PublishedAt: timestamptz(now),
	}); err != nil {
		return fmt.Errorf("publisher: mark %s published: %w", id, err)
	}
	return nil
}

// recordFailure does the backoff bookkeeping for one failed publish of r.
func (p *Publisher) recordFailure(ctx context.Context, tx pgx.Tx, now time.Time, r sqlc.LockDueOutboxRow, cause error) error {
	sctx, cancel := detached(ctx)
	defer cancel()
	p.m.OutboxPublishFailures.Add(sctx, 1)

	attempts := int(r.Attempts) + 1
	message := truncate(cause.Error(), maxErrorLength)
	params := sqlc.RecordOutboxFailureParams{
		ID: r.ID, Status: StatusPending, Attempts: int32(min(attempts, 1<<30)),
		LastError:     pgtype.Text{String: message, Valid: true},
		NextAttemptAt: timestamptz(now.Add(backoff(attempts))),
	}
	failed := attempts >= p.cfg.MaxAttempts
	if failed {
		params.Status, params.NextAttemptAt = StatusFailed, pgtype.Timestamptz{}
	}
	if err := sqlc.New(tx).RecordOutboxFailure(sctx, params); err != nil {
		return fmt.Errorf("publisher: record failure of %s: %w", r.ID, err)
	}
	if !failed {
		p.log.Warn("publisher: publish failed, will retry", "event_id", r.ID, "attempts", attempts, "err", cause)
		return nil
	}

	text := fmt.Sprintf("event %s (%s) not published after %d attempts: %s", r.ID, r.Type, attempts, message)
	if r.Type == TypeSystemError {
		// Reporting a failed system.error with another one would chain while Redis is down.
		p.log.Error("publisher: "+text, "event_id", r.ID)
		return nil
	}
	p.log.Error("publisher: "+text, "event_id", r.ID)
	report := systemErrorEvent(OutboxPublishFailed, text, Subject{DocType: OutboxDocType, Name: r.ID}, r.ID)
	if _, err := Emit(sctx, tx, p.v, now, report); err != nil && !errors.Is(err, ErrEventInvalid) {
		return fmt.Errorf("publisher: report failed event %s: %w", r.ID, err)
	}
	return nil
}

// backoff is the wait before retry number attempts+1: min(2^attempts, 300) seconds.
func backoff(attempts int) time.Duration {
	if attempts >= 9 { // 2^9 s already exceeds the cap
		return maxBackoff
	}
	return min(time.Duration(1<<attempts)*time.Second, maxBackoff)
}

// detached returns a context that outlives ctx's cancellation, bounded by cleanupTime,
// for bookkeeping that must finish once Redis may hold an entry.
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTime)
}

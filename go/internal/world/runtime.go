package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/atipongsena/erp-town/go/internal/platform"
)

type RuntimeConfig struct {
	// Nil supports unscoped custom hosts. Ordinary hosts set the reader's actual
	// ERP company codes, including an explicit empty list to deny every room.
	AllowedCompanies                    []string
	Consumer                            events.ConsumerConfig
	SnapshotInterval, ReconcileInterval time.Duration
}

func DefaultRuntimeConfig(stream string) RuntimeConfig {
	consumer := events.DefaultConsumerConfig(stream, "world", "world")
	consumer.Block = 250 * time.Millisecond // responsive shutdown; blocking reads still wake on every event
	return RuntimeConfig{Consumer: consumer, SnapshotInterval: 30 * time.Second, ReconcileInterval: 30 * time.Second}
}

type companyCheckpoint struct {
	state  State
	cursor string
}

// Runtime restores projection memory independently from the consumer's durable
// dedupe ledger. ERP capability is restricted to read-only Projection by type.
type Runtime struct {
	service     *Service
	rdb         *redis.Client
	pool        *pgxpool.Pool
	metrics     *platform.Metrics
	projection  erp.Projection
	as          erp.Principal
	cfg         RuntimeConfig
	projector   *Projector
	store       *SnapshotStore
	mu          sync.Mutex
	states      map[string]companyCheckpoint
	ready       chan struct{}
	readyOnce   sync.Once
	recoveryErr error
}

func NewRuntime(service *Service, rdb *redis.Client, pool *pgxpool.Pool, metrics *platform.Metrics, projection erp.Projection, as erp.Principal, cfg RuntimeConfig) (*Runtime, error) {
	if service == nil || rdb == nil || pool == nil || metrics == nil || projection == nil || cfg.Consumer.Stream == "" || cfg.Consumer.Group == "" || cfg.SnapshotInterval <= 0 || cfg.ReconcileInterval <= 0 {
		return nil, errors.New("world: incomplete runtime configuration")
	}
	p, err := NewProjector()
	if err != nil {
		return nil, err
	}
	cfg.AllowedCompanies = slices.Clone(cfg.AllowedCompanies)
	return &Runtime{service: service, rdb: rdb, pool: pool, metrics: metrics, projection: projection, as: as, cfg: cfg, projector: p, store: NewSnapshotStore(pool), states: map[string]companyCheckpoint{}, ready: make(chan struct{})}, nil
}

func (r *Runtime) allowsCompany(company string) bool {
	return r.cfg.AllowedCompanies == nil || slices.Contains(r.cfg.AllowedCompanies, company)
}

func (r *Runtime) Current(company string) (State, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	checkpoint, ok := r.states[company]
	return checkpoint.state, ok
}

func (r *Runtime) BeforeJoin(ctx context.Context, company string) error {
	select {
	case <-r.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recoveryErr != nil {
		return r.recoveryErr
	}
	return r.ensureCompany(ctx, company)
}

// Handle is an at-least-once in-memory side effect. The pure reducer makes retries
// idempotent; checkpoints wait for a fully acknowledged stream prefix.
func (r *Runtime) Handle(ctx context.Context, _ pgx.Tx, e events.Envelope) error {
	id := events.StreamID(ctx)
	if _, err := parseSnapshotStreamID(id); err != nil {
		return err
	}
	if e.Company == nil || !r.allowsCompany(*e.Company) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureCompany(ctx, *e.Company); err != nil {
		return err
	}
	checkpoint := r.states[*e.Company]
	state, err := r.projector.Apply(checkpoint.state, e)
	if err != nil {
		return err
	}
	if err := r.service.Publish(ctx, state); err != nil {
		return err
	}
	checkpoint.state = state
	checkpoint.cursor = maxStreamID(checkpoint.cursor, id)
	r.states[*e.Company] = checkpoint
	return nil
}

func (r *Runtime) Run(ctx context.Context) error {
	defer r.service.Close()
	// One world owner per stream: sharing the consumer group between independent
	// room servers would partition events and create incomplete company rooms.
	owner, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer owner.Release()
	var locked bool
	lockName := "world.owner/" + r.cfg.Consumer.Stream + "/" + r.cfg.Consumer.Group
	if err := owner.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", lockName).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("world: another owner is running for this stream")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = owner.Exec(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", lockName)
	}()
	if err := r.Recover(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	consumer := events.NewConsumer(r.rdb, r.pool, r.projector.validator, r.metrics, r.cfg.Consumer, r.Handle)
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()
	snapshot := time.NewTicker(r.cfg.SnapshotInterval)
	defer snapshot.Stop()
	reconcile := time.NewTicker(r.cfg.ReconcileInterval)
	defer reconcile.Stop()
	// ERP reads happen outside the event mutex, never delaying normal delivery.
	var refresh sync.WaitGroup
	refreshDone := make(chan struct{}, 1)
	refreshing := false
	var runErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case runErr = <-done:
			cancel()
			done = nil
			break loop
		case <-snapshot.C:
			if err := r.Checkpoint(ctx); err != nil && ctx.Err() == nil {
				slog.Error("world checkpoint failed", "err", err)
			}
		case <-reconcile.C:
			if !refreshing {
				refreshing = true
				refresh.Go(func() { r.refresh(ctx); refreshDone <- struct{}{} })
			}
		case <-refreshDone:
			refreshing = false
		}
	}
	cancel()
	if done != nil {
		runErr = <-done
	}
	refresh.Wait()
	cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := r.Checkpoint(cleanup); err != nil {
		slog.Warn("world final checkpoint failed", "err", err)
	}
	return runErr
}

func (r *Runtime) refresh(ctx context.Context) {
	r.mu.Lock()
	companies := make([]string, 0, len(r.states))
	for company := range r.states {
		companies = append(companies, company)
	}
	r.mu.Unlock()
	for _, company := range companies {
		current, err := r.projection.ReadProjection(ctx, r.as, company)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("world reconciliation read failed", "company", company, "err", err)
			}
			continue
		}
		r.mu.Lock()
		checkpoint := r.states[company]
		state, err := Reconcile(checkpoint.state, current, time.Now())
		if err == nil {
			err = r.service.Publish(ctx, state)
			if err == nil {
				checkpoint.state = state
				r.states[company] = checkpoint
			}
		}
		r.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			slog.Warn("world reconciliation rejected", "company", company, "err", err)
		}
	}
}

// Checkpoint captures the group cursor before checking pending work while holding
// the event mutex. A later delivery can only move beyond this captured prefix.
// Any pending entry conservatively postpones all snapshots, rather than skipping
// a retryable hole. Database writes happen after the immutable states are captured.
func (r *Runtime) Checkpoint(ctx context.Context) error {
	r.mu.Lock()
	groups, err := r.rdb.XInfoGroups(ctx, r.cfg.Consumer.Stream).Result()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	var cursor string
	for _, group := range groups {
		if group.Name == r.cfg.Consumer.Group {
			cursor = group.LastDeliveredID
			break
		}
	}
	if cursor == "" {
		r.mu.Unlock()
		return errors.New("world: checkpoint group is missing")
	}
	pending, err := r.rdb.XPending(ctx, r.cfg.Consumer.Stream, r.cfg.Consumer.Group).Result()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if pending.Count != 0 {
		r.mu.Unlock()
		return nil
	}
	snapshots := make([]Snapshot, 0, len(r.states))
	for _, checkpoint := range r.states {
		snapshots = append(snapshots, Snapshot{State: checkpoint.state, StreamID: maxStreamID(cursor, checkpoint.cursor), TakenAt: time.Now()})
	}
	r.mu.Unlock()
	for _, snapshot := range snapshots {
		if err := r.store.Save(ctx, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func maxStreamID(a, b string) string {
	aPos, aErr := parseSnapshotStreamID(a)
	bPos, bErr := parseSnapshotStreamID(b)
	if aErr != nil {
		return b
	}
	if bErr != nil || bPos.less(aPos) {
		return a
	}
	return b
}

func resetForRebuild(old State, company string) State {
	state, _ := NewState(company)
	if old.Company() != "" {
		state.version = old.version + 1
		state.seqs = maps.Clone(old.seqs)
		state.reconciled = maps.Clone(old.reconciled)
	}
	return state
}

func (r *Runtime) rebuild(ctx context.Context, company string, old State) (State, error) {
	current, err := r.projection.ReadProjection(ctx, r.as, company)
	if err != nil {
		return State{}, fmt.Errorf("world: rebuild %s: %w", company, err)
	}
	return Reconcile(resetForRebuild(old, company), current, time.Now())
}

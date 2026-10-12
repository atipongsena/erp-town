package agents

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/atipongsena/erp-town/go/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type JobArgs struct {
	RunID string `json:"run_id"`
}

func (JobArgs) Kind() string { return "town_agent_run" }

type Queue struct {
	client *river.Client[pgx.Tx]
	ledger *PostgresLedger
}

// NewQueue does not run migrations or start workers. A configured host must
// explicitly provide its database and worker factory after operational approval.
func NewQueue(pool *pgxpool.Pool) (*Queue, error) {
	return newQueue(pool, nil)
}

// NewWorkerQueue is opt-in: the caller supplies its server-bound run factory.
// It assumes explicitly provisioned schemas and never migrates a database.
func NewWorkerQueue(pool *pgxpool.Pool, factory RunFactory) (*Queue, error) {
	if factory == nil {
		return nil, ErrInvalidRun
	}
	return newQueue(pool, factory)
}

func newQueue(pool *pgxpool.Pool, factory RunFactory) (*Queue, error) {
	if pool == nil {
		return nil, ErrInvalidRun
	}
	var schema string
	if err := pool.QueryRow(context.Background(), "SELECT current_schema()").Scan(&schema); err != nil {
		return nil, errPolicyStorage
	}
	ledger := NewPostgresLedger(pool)
	cfg := &river.Config{Schema: schema, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if factory != nil {
		workers := river.NewWorkers()
		river.AddWorker(workers, NewWorker(ledger, factory))
		cfg.Workers = workers
		cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}}
	}
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, errPolicyStorage
	}
	return &Queue{client: client, ledger: ledger}, nil
}

func (q *Queue) Start(ctx context.Context) error { return q.client.Start(ctx) }
func (q *Queue) Stop(ctx context.Context) error  { return q.client.Stop(ctx) }

func (q *Queue) Enqueue(ctx context.Context, id, agent, company, delegator string) error {
	if !validID(id) || !validID(agent) || !validName(company) || !validName(delegator) {
		return ErrInvalidRun
	}
	tx, err := q.ledger.pool.Begin(ctx)
	if err != nil {
		return errPolicyStorage
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	n, err := sqlc.New(tx).CreateAgentRun(ctx, sqlc.CreateAgentRunParams{ID: id, AgentKey: agent, Company: company, Delegator: delegator})
	if err != nil {
		return errPolicyStorage
	}
	if n == 0 {
		return ErrAlreadyExecuted
	}
	if _, err := q.client.InsertTx(ctx, tx, JobArgs{RunID: id}, &river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true}}); err != nil {
		return errPolicyStorage
	}
	if err := emitAgentRun(ctx, tx, sqlc.LockAgentRunRow{ID: id, AgentKey: agent, Company: company}, "queued", ""); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return errPolicyStorage
	}
	return nil
}

func emitAgentRun(ctx context.Context, tx pgx.Tx, run sqlc.LockAgentRunRow, state string, reason Reason) error {
	typ := "agent.run." + state
	if state == "queued" || state == "running" {
		typ = "agent.run.started"
	}
	if state == "cancelled" {
		typ = "agent.run.failed"
	}
	code := string(reason)
	if code == "" {
		code = "OK"
	}
	return emitAgentEvent(ctx, tx, run, typ, "Town Agent Run", run.ID, map[string]any{"agent_key": run.AgentKey, "state": state, "code": code})
}

// emitAgentWork describes a claimed typed ERP capability, never provider text,
// arguments, results, document names or the human delegator. It shares the run's
// role/company boundary and transaction with the durable tool intent/outcome.
func emitAgentWork(ctx context.Context, tx pgx.Tx, run sqlc.LockAgentRunRow, tool, activity string) error {
	department := agentWorkDepartment(tool)
	if department == "" {
		return nil
	}
	return emitAgentEvent(ctx, tx, run, "agent.run.started", "Town Agent Run", run.ID, map[string]any{
		"agent_key": run.AgentKey, "state": "running", "code": "OK", "department": department, "activity": activity,
	})
}

func agentWorkDepartment(tool string) string {
	for _, cap := range capabilities {
		if cap.name != tool {
			continue
		}
		switch cap.dt {
		case "Sales Order":
			return "shop"
		case "Delivery Note":
			return "warehouse"
		case "Sales Invoice", "Payment Entry":
			return "bank"
		}
		break
	}
	return ""
}

func emitAgentEvent(ctx context.Context, tx pgx.Tx, run sqlc.LockAgentRunRow, typ, dt, name string, data any) error {
	v, err := events.NewValidator()
	if err != nil {
		return errPolicyStorage
	}
	_, err = events.Emit(ctx, tx, v, time.Now(), events.EmitInput{Type: typ, Source: "town/agents", Company: &run.Company, Actor: events.Actor{Kind: "agent", ID: run.AgentKey}, Subject: events.Subject{DocType: dt, Name: name}, CorrelationID: run.ID, Visibility: events.Visibility{Level: "roles", Roles: []string{"System Manager"}}, Data: data})
	if err != nil {
		return errPolicyStorage
	}
	return nil
}

type (
	RunFactory func(context.Context, string) (*Run, error)
	Worker     struct {
		river.WorkerDefaults[JobArgs]
		ledger  *PostgresLedger
		factory RunFactory
	}
)

func NewWorker(l *PostgresLedger, f RunFactory) *Worker { return &Worker{ledger: l, factory: f} }

func (w *Worker) Timeout(*river.Job[JobArgs]) time.Duration { return 2*time.Minute + 10*time.Second }

func (w *Worker) Work(ctx context.Context, job *river.Job[JobArgs]) error {
	if w.ledger == nil || w.factory == nil || job == nil || !validID(job.Args.RunID) {
		return river.JobCancel(ErrInvalidRun)
	}
	state, err := w.ledger.Start(ctx, job.Args.RunID)
	if err != nil {
		return errPolicyStorage
	}
	if state == RunClaimBusy {
		return river.JobSnooze(5 * time.Second)
	}
	if state != RunClaimNew {
		return nil
	}
	stored, err := sqlc.New(w.ledger.pool).LockAgentRun(ctx, job.Args.RunID)
	if err != nil {
		return errPolicyStorage
	}
	// Nothing reconstructed by a retry can reclaim this run. The factory provides
	// an ephemeral, server-bound prompt and credentials, never durable job args.
	run, err := invoke(func() (*Run, error) { return w.factory(ctx, job.Args.RunID) })
	result := Result{Status: NeedsHuman, Reason: Reason("INVALID_RUN")}
	if err == nil && run != nil && run.spec.ID == job.Args.RunID {
		policy, ok := run.executor.(*PolicyExecutor)
		pl, durable := policyLedger(policy)
		if ok && durable && pl.pool == w.ledger.pool && policy.binding.RunID == job.Args.RunID && policy.binding.AgentKey == stored.AgentKey && policy.binding.Company == stored.Company && policy.Validate(ctx) == nil {
			_, delegate, _, _, authErr := policy.authorities(ctx)
			if authErr != nil || delegate.ID != stored.Delegator {
				return w.completeInvalid(ctx, job.Args.RunID)
			}
			result, err = run.Execute(ctx)
			if err != nil {
				result = Result{Status: NeedsHuman, Reason: Reason("INVALID_RUN")}
			}
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return w.ledger.CompleteRun(cleanup, job.Args.RunID, result)
}

func policyLedger(p *PolicyExecutor) (*PostgresLedger, bool) {
	if p == nil {
		return nil, false
	}
	l, ok := p.ledger.(*PostgresLedger)
	return l, ok
}

func (w *Worker) completeInvalid(ctx context.Context, id string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return w.ledger.CompleteRun(cleanup, id, Result{Status: NeedsHuman, Reason: Reason("INVALID_RUN")})
}

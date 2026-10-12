package agents

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresLedger struct{ pool *pgxpool.Pool }

func NewPostgresLedger(pool *pgxpool.Pool) *PostgresLedger { return &PostgresLedger{pool: pool} }

type RunClaimState string

const (
	RunClaimNew      RunClaimState = "new"
	RunClaimBusy     RunClaimState = "busy"
	RunClaimHuman    RunClaimState = "needs_human"
	RunClaimFinished RunClaimState = "finished"
)

func (l *PostgresLedger) Create(ctx context.Context, id, agent, company, delegator string) error {
	if !validID(id) || !validID(agent) || !validName(company) || !validName(delegator) {
		return ErrInvalidRun
	}
	n, err := sqlc.New(l.pool).CreateAgentRun(ctx, sqlc.CreateAgentRunParams{ID: id, AgentKey: agent, Company: company, Delegator: delegator})
	if err != nil {
		return errPolicyStorage
	}
	if n == 0 {
		return ErrAlreadyExecuted
	}
	return nil
}

func (l *PostgresLedger) Start(ctx context.Context, id string) (RunClaimState, error) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return "", errPolicyStorage
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := sqlc.New(tx)
	run, err := q.LockAgentRun(ctx, id)
	if err != nil {
		return "", errPolicyStorage
	}
	state := RunClaimFinished
	switch run.Status {
	case "queued":
		if err := q.StartAgentRun(ctx, id); err != nil {
			return "", errPolicyStorage
		}
		if err := emitAgentRun(ctx, tx, run, "running", ""); err != nil {
			return "", err
		}
		state = RunClaimNew
	case "running":
		if run.LeaseValid {
			return RunClaimBusy, nil
		}
		if err := q.LoseAgentRun(ctx, id); err != nil {
			return "", errPolicyStorage
		}
		if err := emitAgentRun(ctx, tx, run, "needs_human", Reason("WORKER_LOST")); err != nil {
			return "", err
		}
		state = RunClaimHuman
	case "needs_human":
		state = RunClaimHuman
	}
	if err := tx.Commit(ctx); err != nil {
		return "", errPolicyStorage
	}
	return state, nil
}

func (l *PostgresLedger) Attempt(ctx context.Context, agent, company string) (bool, error) {
	if !validID(agent) || !validName(company) {
		return false, ErrInvalidRun
	}
	_, err := sqlc.New(l.pool).AgentRateAttempt(ctx, sqlc.AgentRateAttemptParams{AgentKey: agent, Company: company})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errPolicyStorage
	}
	return true, nil
}

func (l *PostgresLedger) Claim(ctx context.Context, in Intent) (Claim, error) {
	if !validID(in.RunID) || !validID(in.CallID) || !validID(in.Tool) || len(in.Hash) != 64 {
		return Claim{}, ErrInvalidRun
	}
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return Claim{}, errPolicyStorage
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := sqlc.New(tx)
	run, err := q.LockAgentRun(ctx, in.RunID)
	if err != nil {
		return Claim{}, errPolicyStorage
	}
	if run.AgentKey != in.AgentKey || run.Company != in.Company || run.Delegator != in.Delegator {
		return Claim{State: ClaimConflict}, nil
	}
	old, err := q.GetAgentCall(ctx, sqlc.GetAgentCallParams{RunID: in.RunID, CallID: in.CallID})
	if err == nil {
		if old.Tool != in.Tool || old.IntentHash != in.Hash || old.IsWrite != in.Write {
			return Claim{State: ClaimConflict}, nil
		}
		if old.Status == "completed" || old.Status == "needs_human" {
			var result SafeResult
			if json.Unmarshal(old.Result, &result) != nil || !safeOutcome(result) {
				return Claim{}, errPolicyStorage
			}
			return Claim{State: ClaimCompleted, Result: result}, nil
		}
		return Claim{State: ClaimUncertain}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Claim{}, errPolicyStorage
	}
	if run.Status != "running" || !run.LeaseValid || run.CallCount >= 16 {
		return Claim{State: ClaimUncertain}, nil
	}
	if err := q.CreateAgentCall(ctx, sqlc.CreateAgentCallParams{RunID: in.RunID, CallID: in.CallID, Tool: in.Tool, IntentHash: in.Hash, IsWrite: in.Write}); err != nil {
		return Claim{}, errPolicyStorage
	}
	if err := q.CountAgentCall(ctx, in.RunID); err != nil {
		return Claim{}, errPolicyStorage
	}
	if err := q.RecordAgentAudit(ctx, sqlc.RecordAgentAuditParams{RunID: in.RunID, CallID: in.CallID, Tool: in.Tool, Phase: "started"}); err != nil {
		return Claim{}, errPolicyStorage
	}
	if err := emitAgentWork(ctx, tx, run, in.Tool, "working"); err != nil {
		return Claim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Claim{}, errPolicyStorage
	}
	return Claim{State: ClaimNew}, nil
}

func safeOutcome(r SafeResult) bool {
	if r.NeedsHuman {
		return slices.Contains([]string{"TOOL_UNCERTAIN", "AUTHORITY_REVOKED", "SOURCE_CHANGED"}, r.Code) && r.Ref == (erp.DocRef{}) && r.Status == ""
	}
	return r.Code == "OK" && validName(r.Ref.Name) && slices.Contains([]erp.DocType{erp.DocSalesOrder, erp.DocDeliveryNote, erp.DocSalesInvoice, erp.DocPaymentEntry}, r.Ref.DocType) && slices.Contains([]erp.DocStatus{erp.DocDraft, erp.DocSubmitted, erp.DocCancelled}, r.Status)
}

func (l *PostgresLedger) Finish(ctx context.Context, in Intent, r SafeResult) error {
	if !safeOutcome(r) {
		return ErrInvalidRun
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > 2048 {
		return ErrInvalidRun
	}
	phase := "completed"
	if r.NeedsHuman {
		phase = "needs_human"
	}
	if r.Code == "TOOL_UNCERTAIN" {
		phase = "uncertain"
	}
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return errPolicyStorage
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := sqlc.New(tx)
	run, err := q.LockAgentRun(ctx, in.RunID)
	if err != nil || run.AgentKey != in.AgentKey || run.Company != in.Company || run.Delegator != in.Delegator {
		return errPolicyStorage
	}
	n, err := q.FinishAgentCall(ctx, sqlc.FinishAgentCallParams{RunID: in.RunID, CallID: in.CallID, Tool: in.Tool, IntentHash: in.Hash, Status: phase, Result: raw})
	if err != nil || n != 1 {
		return errPolicyStorage
	}
	if err := q.RecordAgentAudit(ctx, sqlc.RecordAgentAuditParams{RunID: in.RunID, CallID: in.CallID, Tool: in.Tool, Phase: phase}); err != nil {
		return errPolicyStorage
	}
	if run.Status == "running" {
		if err := emitAgentWork(ctx, tx, run, in.Tool, "idle"); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errPolicyStorage
	}
	return nil
}

func (l *PostgresLedger) Record(ctx context.Context, e AuditEntry) error {
	if !validID(e.RunID) || !validID(e.CallID) || !validID(e.Tool) || !slices.Contains([]string{"started", "completed", "cancelled", "uncertain", "needs_human"}, e.Phase) {
		return ErrInvalidRun
	}
	if err := sqlc.New(l.pool).RecordAgentAudit(ctx, sqlc.RecordAgentAuditParams{RunID: e.RunID, CallID: e.CallID, Tool: e.Tool, Phase: e.Phase}); err != nil {
		return errPolicyStorage
	}
	return nil
}

// CompleteRun records only safe status, never model text or prompt. A run with
// any unresolved tool cannot award AI progress or claim successful completion.
func (l *PostgresLedger) CompleteRun(ctx context.Context, id string, result Result) error {
	if !slices.Contains([]Status{Completed, NeedsHuman, Cancelled, Failed}, result.Status) || !slices.Contains([]Reason{"", TurnLimit, ToolLimit, TokenLimit, PayloadLimit, InvalidResponse, UnknownTool, CallConflict, ToolUncertain, HumanRequired, AuditUnavailable, ProviderUnavailable, "WORKER_LOST", "AUTHORITY_REVOKED", "INVALID_RUN"}, result.Reason) {
		return ErrInvalidRun
	}
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return errPolicyStorage
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := sqlc.New(tx)
	run, err := q.LockAgentRun(ctx, id)
	if err != nil {
		return errPolicyStorage
	}
	unresolved, err := q.HasUnresolvedAgentCalls(ctx, id)
	if err != nil {
		return errPolicyStorage
	}
	if unresolved {
		result.Status, result.Reason = NeedsHuman, ToolUncertain
	}
	denied, err := q.HasDeniedAgentCalls(ctx, id)
	if err != nil {
		return errPolicyStorage
	}
	if denied && result.Status == Completed {
		result.Status, result.Reason = NeedsHuman, HumanRequired
	}
	n, err := q.FinishAgentRun(ctx, sqlc.FinishAgentRunParams{ID: id, Status: string(result.Status), Reason: string(result.Reason)})
	if err != nil {
		return errPolicyStorage
	}
	if n == 1 && result.Status == Completed && run.CallCount > 0 {
		progress, err := q.IncrementAgentProgress(ctx, sqlc.IncrementAgentProgressParams{AgentKey: run.AgentKey, Company: run.Company})
		if err != nil {
			return errPolicyStorage
		}
		if err := emitAgentEvent(ctx, tx, run, "agent.progress.updated", "Town Agent", run.AgentKey, map[string]any{"agent_key": run.AgentKey, "successful_runs": progress}); err != nil {
			return err
		}
	}
	if n == 1 {
		if err := emitAgentRun(ctx, tx, run, string(result.Status), result.Reason); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errPolicyStorage
	}
	return nil
}

func (l *PostgresLedger) Purge(ctx context.Context, before time.Time) (int64, error) {
	// A caller cannot shorten the approved retention boundary.
	maxBefore := time.Now().Add(-400 * 24 * time.Hour)
	if before.After(maxBefore) {
		return 0, ErrInvalidRun
	}
	q := sqlc.New(l.pool)
	n, err := q.PurgeCompletedAgentRuns(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, errPolicyStorage
	}
	if err := q.PurgeOldAgentRateWindows(ctx); err != nil {
		return n, errPolicyStorage
	}
	return n, nil
}

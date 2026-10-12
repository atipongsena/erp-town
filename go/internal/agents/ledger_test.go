package agents

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ledgerFixture(t *testing.T) (*PostgresLedger, Intent) {
	t.Helper()
	l := NewPostgresLedger(storetest.NewPool(t))
	in := Intent{RunID: "RUN-1", CallID: "CALL-1", Tool: "create_sales_order_draft", Hash: "0123456789012345678901234567890123456789012345678901234567890123", AgentKey: "helper", Company: "ACME", Delegator: "human@test", Write: true}
	require.NoError(t, l.Create(t.Context(), in.RunID, in.AgentKey, in.Company, in.Delegator))
	state, err := l.Start(t.Context(), in.RunID)
	require.NoError(t, err)
	require.Equal(t, RunClaimNew, state)
	return l, in
}

func TestPostgresLedgerCompetingWorkersClaimOnce(t *testing.T) {
	l, in := ledgerFixture(t)
	var fresh atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			claim, err := l.Claim(t.Context(), in)
			errs <- err
			if claim.State == ClaimNew {
				fresh.Add(1)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), fresh.Load())
	var n int
	require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_tool_calls").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestPostgresLedgerRestartNeverReclaimsUncertainWrite(t *testing.T) {
	l, in := ledgerFixture(t)
	claim, err := l.Claim(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, ClaimNew, claim.State)
	restarted := NewPostgresLedger(l.pool)
	claim, err = restarted.Claim(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, ClaimUncertain, claim.State)
	_, err = l.pool.Exec(t.Context(), "UPDATE agent_runs SET lease_until=clock_timestamp()-interval '1 second'")
	require.NoError(t, err)
	state, err := restarted.Start(t.Context(), in.RunID)
	require.NoError(t, err)
	assert.Equal(t, RunClaimHuman, state)
	claim, err = restarted.Claim(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, ClaimUncertain, claim.State)
}

func TestPostgresLedgerOutcomeAndCanonicalHashSurviveRestart(t *testing.T) {
	l, in := ledgerFixture(t)
	_, err := l.Claim(t.Context(), in)
	require.NoError(t, err)
	result := SafeResult{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: "SO-1"}, Status: erp.DocDraft, Code: "OK"}
	require.NoError(t, l.Finish(t.Context(), in, result))
	claim, err := NewPostgresLedger(l.pool).Claim(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, ClaimCompleted, claim.State)
	assert.Equal(t, result, claim.Result)
	in.Hash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	claim, err = l.Claim(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, ClaimConflict, claim.State)
	var stored string
	require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT result::text FROM agent_tool_calls").Scan(&stored))
	assert.NotContains(t, stored, "human@test")
	assert.NotContains(t, stored, "amount")
}

func TestPostgresLedgerAtomicRateLimit(t *testing.T) {
	l, _ := ledgerFixture(t)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 80)
	for range 80 {
		wg.Go(func() {
			ok, err := l.Attempt(t.Context(), "helper", "ACME")
			errs <- err
			if ok {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(30), allowed.Load())
	ok, err := l.Attempt(t.Context(), "helper", "OTHER")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPostgresLedgerRunScopeAndCallLimit(t *testing.T) {
	l, in := ledgerFixture(t)
	bad := in
	bad.Company = "OTHER"
	claim, err := l.Claim(t.Context(), bad)
	require.NoError(t, err)
	assert.Equal(t, ClaimConflict, claim.State)
	for i := range 16 {
		next := in
		next.CallID = string(rune('a' + i))
		claim, err := l.Claim(t.Context(), next)
		require.NoError(t, err)
		assert.Equal(t, ClaimNew, claim.State)
	}
	in.CallID = "overflow"
	claim, err = l.Claim(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, ClaimUncertain, claim.State)
}

func TestPostgresLedgerCancelledOutcomeStillRecorded(t *testing.T) {
	l, in := ledgerFixture(t)
	_, err := l.Claim(t.Context(), in)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	x := &PolicyExecutor{ledger: l}
	require.NoError(t, x.finish(ctx, in, SafeResult{Code: "TOOL_UNCERTAIN", NeedsHuman: true}))
	claim, err := l.Claim(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, ClaimUncertain, claim.State)
}

func TestPostgresLedgerRetentionPreservesUnresolvedWrites(t *testing.T) {
	l, in := ledgerFixture(t)
	_, err := l.Claim(t.Context(), in)
	require.NoError(t, err)
	_, err = l.pool.Exec(t.Context(), "UPDATE agent_runs SET status='needs_human',finished_at=clock_timestamp()-interval '401 days'")
	require.NoError(t, err)
	n, err := l.Purge(t.Context(), time.Now().Add(-400*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	require.NoError(t, l.Finish(t.Context(), in, SafeResult{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: "SO-1"}, Status: erp.DocDraft, Code: "OK"}))
	n, err = l.Purge(t.Context(), time.Now().Add(-400*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestPostgresLedgerCannotAwardProgressForDeniedCall(t *testing.T) {
	for _, code := range []string{"AUTHORITY_REVOKED", "SOURCE_CHANGED"} {
		t.Run(code, func(t *testing.T) {
			l, in := ledgerFixture(t)
			_, err := l.Claim(t.Context(), in)
			require.NoError(t, err)
			require.NoError(t, l.Finish(t.Context(), in, SafeResult{Code: code, NeedsHuman: true}))
			require.NoError(t, l.CompleteRun(t.Context(), in.RunID, Result{Status: Completed}))
			var count int
			require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_progress").Scan(&count))
			assert.Zero(t, count)
			var status string
			require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT status FROM agent_runs").Scan(&status))
			assert.Equal(t, "needs_human", status)
		})
	}
}

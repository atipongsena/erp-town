package agents

import (
	"context"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func queueFixture(t *testing.T) *Queue {
	t.Helper()
	pool := storetest.NewPool(t)
	// Explicit migrations only inside this isolated schema. Queue construction
	// and application startup do not run River migrations against dev or pilot.
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	require.NoError(t, err)
	_, err = migrator.Migrate(t.Context(), rivermigrate.DirectionUp, nil)
	require.NoError(t, err)
	q, err := NewQueue(pool)
	require.NoError(t, err)
	return q
}

func TestPollingQueueExecutesAndShutsDownWithoutDuplicateEffects(t *testing.T) {
	q := queueFixture(t)
	x, _, _ := policyFixture(t)
	x.ledger = q.ledger
	done := make(chan struct{}, 1)
	factory := func(ctx context.Context, id string) (*Run, error) {
		return NewRun(Spec{ID: id, Instruction: "Prepare a draft.", Tools: x.Tools()}, providerFunc(func(context.Context, Request) (Response, error) {
			select {
			case done <- struct{}{}:
			default:
			}
			return finished(), nil
		}), x, q.ledger, DefaultLimits())
	}
	poller, err := NewWorkerQueue(q.ledger.pool, factory)
	require.NoError(t, err)
	require.NoError(t, poller.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, poller.Stop(ctx))
	})
	require.NoError(t, poller.Enqueue(t.Context(), "RUN-1", "sales_helper", "ACME", "human@test"))
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("River did not execute configured worker")
	}
	require.Eventually(t, func() bool {
		var status string
		err := q.ledger.pool.QueryRow(t.Context(), "SELECT status FROM agent_runs WHERE id='RUN-1'").Scan(&status)
		return err == nil && status == "completed"
	}, 5*time.Second, 20*time.Millisecond)
}

func TestQueueWorkerRejectsWrongCompanyBeforeProvider(t *testing.T) {
	q := queueFixture(t)
	require.NoError(t, q.Enqueue(t.Context(), "RUN-1", "sales_helper", "OTHER", "human@test"))
	x, _, _ := policyFixture(t)
	x.ledger = q.ledger
	factory := func(ctx context.Context, id string) (*Run, error) {
		return NewRun(Spec{ID: id, Instruction: "Read.", Tools: x.Tools()}, providerFunc(func(context.Context, Request) (Response, error) {
			t.Error("wrong company reached provider")
			return finished(), nil
		}), x, q.ledger, DefaultLimits())
	}
	require.NoError(t, NewWorker(q.ledger, factory).Work(t.Context(), &river.Job[JobArgs]{Args: JobArgs{RunID: "RUN-1"}}))
	var status string
	require.NoError(t, q.ledger.pool.QueryRow(t.Context(), "SELECT status FROM agent_runs").Scan(&status))
	assert.Equal(t, "needs_human", status)
}

func TestQueueEnqueueCommitsRunJobAndOutboxTogether(t *testing.T) {
	q := queueFixture(t)
	require.NoError(t, q.Enqueue(t.Context(), "RUN-1", "helper", "ACME", "human@test"))
	for _, table := range []string{"agent_runs", "river_job", "town_outbox"} {
		var n int
		require.NoError(t, q.ledger.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n))
		assert.Equal(t, 1, n)
	}
	var args string
	require.NoError(t, q.ledger.pool.QueryRow(t.Context(), "SELECT args::text FROM river_job").Scan(&args))
	assert.JSONEq(t, `{"run_id":"RUN-1"}`, args)
	require.ErrorIs(t, q.Enqueue(t.Context(), "RUN-1", "other", "OTHER", "another@test"), ErrAlreadyExecuted)
}

func TestQueueFailedInsertRollsBackRunAndOutbox(t *testing.T) {
	q := queueFixture(t)
	_, err := q.ledger.pool.Exec(t.Context(), "DROP TABLE river_job CASCADE")
	require.NoError(t, err)
	require.Error(t, q.Enqueue(t.Context(), "RUN-1", "helper", "ACME", "human@test"))
	for _, table := range []string{"agent_runs", "town_outbox", "town_subject_seq"} {
		var n int
		require.NoError(t, q.ledger.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n))
		assert.Zero(t, n)
	}
}

func TestQueueWorkerCannotRepeatCompletedRun(t *testing.T) {
	q := queueFixture(t)
	require.NoError(t, q.Enqueue(t.Context(), "RUN-1", "sales_helper", "ACME", "human@test"))
	x, _, _ := policyFixture(t)
	x.ledger = q.ledger
	count := 0
	factory := func(ctx context.Context, id string) (*Run, error) {
		count++
		return NewRun(Spec{ID: id, Instruction: "Prepare a draft.", Tools: x.Tools()}, providerFunc(func(ctx context.Context, req Request) (Response, error) {
			if len(req.History) == 0 {
				return Response{Calls: []Call{draftCall(draftArgs)}}, nil
			}
			return finished(), nil
		}), x, q.ledger, DefaultLimits())
	}
	worker := NewWorker(q.ledger, factory)
	job := &river.Job[JobArgs]{Args: JobArgs{RunID: "RUN-1"}}
	require.NoError(t, worker.Work(t.Context(), job))
	require.NoError(t, worker.Work(t.Context(), job))
	assert.Equal(t, 1, count)
	var n int
	require.NoError(t, q.ledger.pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_tool_calls").Scan(&n))
	assert.Equal(t, 1, n)
	var progress int
	require.NoError(t, q.ledger.pool.QueryRow(t.Context(), "SELECT successful_runs FROM agent_progress").Scan(&progress))
	assert.Equal(t, 1, progress)
}

func TestQueueWorkerLostLeaseNeverInvokesFactory(t *testing.T) {
	q := queueFixture(t)
	require.NoError(t, q.Enqueue(t.Context(), "RUN-1", "helper", "ACME", "human@test"))
	_, err := q.ledger.Start(t.Context(), "RUN-1")
	require.NoError(t, err)
	_, err = q.ledger.pool.Exec(t.Context(), "UPDATE agent_runs SET lease_until=clock_timestamp()-interval '1 second'")
	require.NoError(t, err)
	worker := NewWorker(q.ledger, func(context.Context, string) (*Run, error) {
		t.Error("lost run must not be reconstructed")
		return nil, ErrInvalidRun
	})
	require.NoError(t, worker.Work(t.Context(), &river.Job[JobArgs]{Args: JobArgs{RunID: "RUN-1"}}))
	var status string
	require.NoError(t, q.ledger.pool.QueryRow(t.Context(), "SELECT status FROM agent_runs").Scan(&status))
	assert.Equal(t, "needs_human", status)
}

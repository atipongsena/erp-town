-- name: CreateAgentRun :execrows
INSERT INTO agent_runs(id,agent_key,company,delegator) VALUES($1,$2,$3,$4)
ON CONFLICT (id) DO NOTHING;

-- name: LockAgentRun :one
SELECT *, COALESCE(lease_until > clock_timestamp(),false)::boolean AS lease_valid FROM agent_runs WHERE id=$1 FOR UPDATE;

-- name: StartAgentRun :exec
UPDATE agent_runs SET status='running',lease_until=clock_timestamp()+interval '2 minutes' WHERE id=$1 AND status='queued';

-- name: LoseAgentRun :exec
UPDATE agent_runs SET status='needs_human',reason='WORKER_LOST',finished_at=clock_timestamp()
WHERE id=$1 AND status='running';

-- name: GetAgentCall :one
SELECT * FROM agent_tool_calls WHERE run_id=$1 AND call_id=$2;

-- name: CountAgentCall :exec
UPDATE agent_runs SET call_count=call_count+1 WHERE id=$1;

-- name: CreateAgentCall :exec
INSERT INTO agent_tool_calls(run_id,call_id,tool,intent_hash,is_write,status) VALUES($1,$2,$3,$4,$5,'started');

-- name: FinishAgentCall :execrows
UPDATE agent_tool_calls SET status=$5,result=$6,finished_at=clock_timestamp()
WHERE run_id=$1 AND call_id=$2 AND tool=$3 AND intent_hash=$4 AND status='started';

-- name: AgentRateAttempt :one
WITH tick AS (SELECT clock_timestamp() AS ts)
INSERT INTO agent_policy_windows(agent_key,company,accepted_at,updated_at)
VALUES($1,$2,ARRAY[(SELECT ts FROM tick)],(SELECT ts FROM tick))
ON CONFLICT(agent_key,company) DO UPDATE SET
accepted_at=ARRAY(SELECT t FROM unnest(agent_policy_windows.accepted_at) t WHERE t > (SELECT ts FROM tick)-interval '1 minute') || (SELECT ts FROM tick),
updated_at=(SELECT ts FROM tick)
WHERE (SELECT count(*) FROM unnest(agent_policy_windows.accepted_at) t WHERE t > (SELECT ts FROM tick)-interval '1 minute') < 30
RETURNING cardinality(accepted_at)::int AS attempts;

-- name: RecordAgentAudit :exec
INSERT INTO agent_run_audit(run_id,call_id,tool,phase) VALUES($1,$2,$3,$4);

-- name: FinishAgentRun :execrows
UPDATE agent_runs SET status=$2,reason=$3,finished_at=clock_timestamp()
WHERE id=$1 AND status='running';

-- name: HasUnresolvedAgentCalls :one
SELECT EXISTS(SELECT 1 FROM agent_tool_calls WHERE run_id=$1 AND status IN ('started','uncertain'));

-- name: HasDeniedAgentCalls :one
SELECT EXISTS(SELECT 1 FROM agent_tool_calls WHERE run_id=$1 AND status='needs_human');

-- name: IncrementAgentProgress :one
INSERT INTO agent_progress(agent_key,company,successful_runs) VALUES($1,$2,1)
ON CONFLICT(agent_key,company) DO UPDATE SET successful_runs=agent_progress.successful_runs+1,updated_at=clock_timestamp()
RETURNING successful_runs;

-- name: PurgeCompletedAgentRuns :execrows
DELETE FROM agent_runs WHERE id IN (
    SELECT r.id FROM agent_runs r WHERE r.finished_at < $1
    AND r.status IN ('completed','needs_human','failed','cancelled')
    AND NOT EXISTS(SELECT 1 FROM agent_tool_calls c WHERE c.run_id=r.id AND c.status IN ('started','uncertain'))
    ORDER BY r.finished_at LIMIT 500 FOR UPDATE SKIP LOCKED
);

-- name: PurgeOldAgentRateWindows :exec
DELETE FROM agent_policy_windows WHERE updated_at < clock_timestamp()-interval '2 minutes';

-- name: ListAgentRunMetadata :many
SELECT r.id, r.agent_key, r.status, r.call_count, r.created_at, r.finished_at,
       EXISTS(SELECT 1 FROM agent_tool_calls c WHERE c.run_id=r.id AND c.status IN ('started','uncertain')) AS unresolved
FROM agent_runs r
WHERE r.company=$1
ORDER BY (r.status='needs_human') DESC, r.created_at DESC, r.id DESC
LIMIT 26;

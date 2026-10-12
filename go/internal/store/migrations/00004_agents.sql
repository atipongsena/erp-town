-- +goose Up
CREATE TABLE agent_runs (
    id text PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    agent_key text NOT NULL,
    company text NOT NULL,
    delegator text NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','completed','needs_human','failed','cancelled')),
    reason text NOT NULL DEFAULT '',
    call_count int NOT NULL DEFAULT 0 CHECK (call_count BETWEEN 0 AND 16),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_until timestamptz,
    finished_at timestamptz
);
CREATE INDEX agent_runs_company_created_idx ON agent_runs(company,created_at);
CREATE TABLE agent_tool_calls (
    run_id text NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    call_id text NOT NULL CHECK (length(call_id) BETWEEN 1 AND 128),
    tool text NOT NULL,
    intent_hash text NOT NULL CHECK (intent_hash ~ '^[0-9a-f]{64}$'),
    is_write boolean NOT NULL,
    status text NOT NULL CHECK (status IN ('started','completed','needs_human','uncertain')),
    result jsonb CHECK (result IS NULL OR octet_length(result::text) <= 2048),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    finished_at timestamptz,
    PRIMARY KEY (run_id,call_id)
);
CREATE TABLE agent_policy_windows (
    agent_key text NOT NULL,
    company text NOT NULL,
    accepted_at timestamptz[] NOT NULL CHECK (cardinality(accepted_at) BETWEEN 1 AND 30),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (agent_key,company)
);
CREATE TABLE agent_run_audit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id text NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    call_id text NOT NULL,
    tool text NOT NULL,
    phase text NOT NULL CHECK (phase IN ('started','completed','cancelled','uncertain','needs_human')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE agent_progress (
    agent_key text NOT NULL,
    company text NOT NULL,
    successful_runs bigint NOT NULL DEFAULT 0 CHECK (successful_runs >= 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(agent_key,company)
);

-- +goose Down
DROP TABLE agent_progress;
DROP TABLE agent_run_audit;
DROP TABLE agent_policy_windows;
DROP TABLE agent_tool_calls;
DROP TABLE agent_runs;

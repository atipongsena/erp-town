-- +goose Up
CREATE TABLE world_snapshots (
    company text PRIMARY KEY,
    stream_id text NOT NULL,
    state bytea NOT NULL,
    taken_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE world_snapshots;

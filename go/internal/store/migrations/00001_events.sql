-- +goose Up
CREATE TABLE town_outbox (
    id text PRIMARY KEY,
    type text NOT NULL,
    subject_doctype text NOT NULL,
    subject_name text NOT NULL,
    payload text NOT NULL,
    status text NOT NULL CHECK (status IN ('Pending', 'Published', 'Failed', 'Invalid')),
    attempts int NOT NULL DEFAULT 0,
    last_error text,
    next_attempt_at timestamptz,
    stream_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
CREATE INDEX town_outbox_status_next_attempt_idx ON town_outbox (status, next_attempt_at);

CREATE TABLE town_subject_seq (
    subject_doctype text NOT NULL,
    subject_name text NOT NULL,
    last_seq bigint NOT NULL,
    PRIMARY KEY (subject_doctype, subject_name)
);

CREATE TABLE consumed_events (
    consumer_group text NOT NULL,
    event_id text NOT NULL,
    consumed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer_group, event_id)
);

CREATE TABLE consumer_subject_seq (
    consumer_group text NOT NULL,
    subject_doctype text NOT NULL,
    subject_name text NOT NULL,
    last_seq bigint NOT NULL,
    PRIMARY KEY (consumer_group, subject_doctype, subject_name)
);

-- +goose Down
DROP TABLE consumer_subject_seq;
DROP TABLE consumed_events;
DROP TABLE town_subject_seq;
DROP TABLE town_outbox;

-- name: NextSubjectSeq :one
-- Allocates the subject's next seq (first = 1). The upsert row-locks the subject until
-- the caller's transaction ends, so emitters of one subject serialize, and a rollback
-- gives the seq back.
INSERT INTO town_subject_seq (subject_doctype, subject_name, last_seq)
VALUES ($1, $2, 1)
ON CONFLICT (subject_doctype, subject_name) DO UPDATE SET last_seq = town_subject_seq.last_seq + 1
RETURNING last_seq;

-- name: InsertOutbox :exec
-- created_at and insert_order come from the database at insert time (after the subject's
-- seq lock is held), never from the caller's clock.
INSERT INTO town_outbox (id, type, subject_doctype, subject_name, payload, status, last_error, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, clock_timestamp());

-- name: LockDueOutbox :many
-- Locks up to batch_size due Pending rows in insertion order (so each subject's events
-- go out in seq order); rows another publisher holds are skipped, so no row is
-- published by two publishers at once.
SELECT id, type, payload, attempts
FROM town_outbox
WHERE status = 'Pending'
  AND (next_attempt_at IS NULL OR next_attempt_at <= sqlc.arg(now))
ORDER BY insert_order
LIMIT sqlc.arg(batch_size)
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxPublished :exec
UPDATE town_outbox
SET status = 'Published', stream_id = $2, published_at = $3, next_attempt_at = NULL, last_error = NULL
WHERE id = $1;

-- name: RecordOutboxFailure :exec
UPDATE town_outbox
SET status = $2, attempts = $3, last_error = $4, next_attempt_at = $5
WHERE id = $1;

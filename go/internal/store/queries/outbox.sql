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
-- Locks due subject heads only. An earlier Pending row blocks its subject even
-- during backoff or while another publisher holds its lock. Terminal rows do not.
-- The boundary keeps rows inserted during this publish call for the next batch.
SELECT candidate.id, candidate.type, candidate.payload, candidate.attempts
FROM town_outbox AS candidate
WHERE candidate.status = 'Pending'
  AND candidate.insert_order <= sqlc.arg(boundary)
  AND (candidate.next_attempt_at IS NULL OR candidate.next_attempt_at <= sqlc.arg(now))
  AND NOT EXISTS (
    SELECT 1 FROM town_outbox AS earlier
    WHERE earlier.status = 'Pending'
      AND earlier.subject_doctype = candidate.subject_doctype
      AND earlier.subject_name = candidate.subject_name
      AND earlier.insert_order < candidate.insert_order
  )
ORDER BY candidate.insert_order
LIMIT sqlc.arg(batch_size)
FOR UPDATE OF candidate SKIP LOCKED;

-- name: OutboxBoundary :one
-- Capture the visible backlog's insertion boundary before publishing. New error
-- reports and events inserted beyond this boundary wait for the next call.
SELECT COALESCE(max(insert_order), 0)::bigint FROM town_outbox WHERE status = 'Pending';

-- name: MarkOutboxPublished :exec
UPDATE town_outbox
SET status = 'Published', stream_id = $2, published_at = $3, next_attempt_at = NULL, last_error = NULL
WHERE id = $1;

-- name: PurgePublishedOutbox :execrows
-- Bounded cleanup never waits on another publisher/cleaner or removes unresolved
-- rows. Subject seq counters deliberately survive retention.
WITH expired AS (
    SELECT candidate.id FROM town_outbox AS candidate
    WHERE candidate.status = 'Published' AND candidate.published_at < sqlc.arg(cutoff)
    ORDER BY candidate.published_at, candidate.id
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM town_outbox AS outbox
USING expired
WHERE outbox.id = expired.id;

-- name: RecordOutboxFailure :exec
UPDATE town_outbox
SET status = $2, attempts = $3, last_error = $4, next_attempt_at = $5
WHERE id = $1;

-- name: InsertConsumed :execrows
-- Records that the group consumed the event; 0 rows means it was seen before.
INSERT INTO consumed_events (consumer_group, event_id)
VALUES ($1, $2)
ON CONFLICT (consumer_group, event_id) DO NOTHING;

-- name: EnsureSubjectSeq :exec
-- Creates the subject's row (last_seq 0) so LockSubjectSeq always has a row to lock.
INSERT INTO consumer_subject_seq (consumer_group, subject_doctype, subject_name, last_seq)
VALUES ($1, $2, $3, 0)
ON CONFLICT (consumer_group, subject_doctype, subject_name) DO NOTHING;

-- name: LockSubjectSeq :one
SELECT last_seq
FROM consumer_subject_seq
WHERE consumer_group = $1 AND subject_doctype = $2 AND subject_name = $3
FOR UPDATE;

-- name: SetSubjectSeq :exec
INSERT INTO consumer_subject_seq (consumer_group, subject_doctype, subject_name, last_seq)
VALUES ($1, $2, $3, $4)
ON CONFLICT (consumer_group, subject_doctype, subject_name) DO UPDATE SET last_seq = EXCLUDED.last_seq;

-- name: PurgeConsumed :execrows
DELETE FROM consumed_events
WHERE consumer_group = sqlc.arg(consumer_group)
  AND consumed_at < now() - make_interval(secs => sqlc.arg(window_seconds)::float8);

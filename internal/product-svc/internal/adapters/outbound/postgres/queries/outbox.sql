-- name: AppendOutboxRecord :one
INSERT INTO
  outbox_records (
    event_id,
    event_name,
    aggregate_type,
    aggregate_id,
    topic,
    key,
    payload,
    headers,
    attempt,
    status
  )
VALUES
  (
    sqlc.arg (event_id),
    sqlc.arg (event_name),
    sqlc.arg (aggregate_type),
    sqlc.arg (aggregate_id),
    sqlc.arg (topic),
    sqlc.narg (key),
    sqlc.arg (payload),
    sqlc.arg (headers),
    0,
    sqlc.arg (status)
  )
RETURNING
  *;

-- name: ClaimPendingOutboxRecords :many
UPDATE outbox_records
SET
  status = 'in_progress',
  locked_at = sqlc.arg (claimed_at),
  updated_at = sqlc.arg (claimed_at)
WHERE
  id IN (
    SELECT
      o.id
    FROM
      outbox_records AS o
    WHERE
      (
        o.status IN ('pending', 'failed')
        AND COALESCE(o.next_attempt_at, '-infinity'::timestamptz) <= sqlc.arg (before)
      )
      OR (
        o.status = 'in_progress'
        AND o.locked_at IS NOT NULL
        AND o.locked_at <= sqlc.arg (stale_before)
      )
    ORDER BY
      o.created_at,
      o.id
    LIMIT
      sqlc.arg (limit_count)
    FOR UPDATE
      SKIP LOCKED
  )
RETURNING
  *;

-- name: MarkOutboxRecordPublished :execrows
UPDATE outbox_records
SET
  status = 'published',
  published_at = sqlc.arg (published_at),
  locked_at = NULL,
  updated_at = sqlc.arg (published_at)
WHERE
  id = sqlc.arg (id)
  AND status = 'in_progress'
  AND locked_at = sqlc.arg (claim_token);

-- name: MarkOutboxRecordFailed :execrows
UPDATE outbox_records
SET
  status = 'failed',
  attempt = sqlc.arg (attempt),
  next_attempt_at = sqlc.arg (next_attempt_at),
  last_error = sqlc.arg (last_error),
  locked_at = NULL,
  updated_at = sqlc.arg (updated_at)
WHERE
  id = sqlc.arg (id)
  AND status = 'in_progress'
  AND locked_at = sqlc.arg (claim_token);

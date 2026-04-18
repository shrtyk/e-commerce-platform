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
    sqlc.arg (status)
  )
RETURNING
  *;

-- name: ClaimPendingOutboxRecords :many
WITH candidates AS (
  SELECT
    o.id
  FROM
    outbox_records AS o
  WHERE
    o.status = 'pending'
    AND o.next_attempt_at <= sqlc.arg (before)
  ORDER BY
    o.next_attempt_at,
    o.created_at,
    o.id
  LIMIT
    sqlc.arg (limit_count)
  FOR UPDATE
    SKIP LOCKED
)
UPDATE outbox_records AS o
SET
  status = 'in_progress',
  locked_at = sqlc.arg (claimed_at),
  locked_by = sqlc.arg (locked_by),
  updated_at = sqlc.arg (claimed_at)
FROM
  candidates
WHERE
  o.id = candidates.id
RETURNING
  o.*;

-- name: ClaimStaleInProgressOutboxRecords :many
WITH candidates AS (
  SELECT
    o.id
  FROM
    outbox_records AS o
  WHERE
    o.status = 'in_progress'
    AND o.locked_at <= sqlc.arg (stale_before)
  ORDER BY
    o.locked_at,
    o.created_at,
    o.id
  LIMIT
    sqlc.arg (limit_count)
  FOR UPDATE
    SKIP LOCKED
)
UPDATE outbox_records AS o
SET
  locked_at = sqlc.arg (claimed_at),
  locked_by = sqlc.arg (locked_by),
  updated_at = sqlc.arg (claimed_at)
FROM
  candidates
WHERE
  o.id = candidates.id
RETURNING
  o.*;

-- name: MarkOutboxRecordPublished :execrows
UPDATE outbox_records
SET
  status = 'published',
  published_at = sqlc.arg (published_at),
  locked_at = NULL,
  locked_by = NULL,
  updated_at = sqlc.arg (published_at)
WHERE
  id = sqlc.arg (id)
  AND status = 'in_progress'
  AND locked_by = sqlc.arg (locked_by)
  AND locked_at = sqlc.arg (claim_token);

-- name: MarkOutboxRecordRetryableFailure :execrows
UPDATE outbox_records
SET
  status = 'pending',
  attempt = sqlc.arg (attempt),
  next_attempt_at = sqlc.arg (next_attempt_at),
  last_error = sqlc.arg (last_error),
  locked_at = NULL,
  locked_by = NULL,
  updated_at = sqlc.arg (updated_at)
WHERE
  id = sqlc.arg (id)
  AND status = 'in_progress'
  AND locked_by = sqlc.arg (locked_by)
  AND locked_at = sqlc.arg (claim_token);

-- name: MarkOutboxRecordDead :execrows
UPDATE outbox_records
SET
  status = 'dead',
  attempt = sqlc.arg (attempt),
  next_attempt_at = sqlc.arg (updated_at),
  last_error = sqlc.arg (last_error),
  locked_at = NULL,
  locked_by = NULL,
  updated_at = sqlc.arg (updated_at)
WHERE
  id = sqlc.arg (id)
  AND status = 'in_progress'
  AND locked_by = sqlc.arg (locked_by)
  AND locked_at = sqlc.arg (claim_token);

-- name: CreateSession :one
INSERT INTO
  sessions (user_id, token_hash, expires_at)
VALUES
  ($1, $2, $3)
RETURNING
  session_id,
  user_id,
  token_hash,
  expires_at,
  revoked_at,
  created_at,
  updated_at;

-- name: GetSessionByID :one
SELECT
  session_id,
  user_id,
  token_hash,
  expires_at,
  revoked_at,
  created_at,
  updated_at
FROM
  sessions
WHERE
  session_id = $1;

-- name: RevokeSession :execrows
UPDATE sessions
SET
  revoked_at = $2,
  updated_at = now()
WHERE
  session_id = $1
  AND revoked_at IS NULL;

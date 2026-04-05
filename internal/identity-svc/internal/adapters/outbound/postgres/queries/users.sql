-- name: CreateUser :one
INSERT INTO
  users (email, password_hash, display_name, status)
VALUES
  ($1, $2, $3, $4)
RETURNING
  user_id,
  email,
  password_hash,
  display_name,
  status,
  created_at,
  updated_at;

-- name: GetUserByEmail :one
SELECT
  user_id,
  email,
  password_hash,
  display_name,
  status,
  created_at,
  updated_at
FROM
  users
WHERE
  email = $1
LIMIT
  1;

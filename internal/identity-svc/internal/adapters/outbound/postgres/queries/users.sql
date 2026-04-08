-- name: CreateUser :one
INSERT INTO
  users (email, password_hash, display_name, role_code, status)
VALUES
  ($1, $2, $3, $4, $5)
RETURNING
  user_id,
  email,
  password_hash,
  display_name,
  role_code,
  status,
  created_at,
  updated_at;

-- name: GetUserByEmail :one
SELECT
  user_id,
  email,
  password_hash,
  display_name,
  role_code,
  status,
  created_at,
  updated_at
FROM
  users
WHERE
  email = $1;

-- name: GetUserByID :one
SELECT
  user_id,
  email,
  password_hash,
  display_name,
  role_code,
  status,
  created_at,
  updated_at
FROM
  users
WHERE
  user_id = $1;

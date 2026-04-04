-- name: CreateUser :exec
INSERT INTO users (
    user_id,
    email,
    password_hash,
    display_name,
    status,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetUserByEmail :one
SELECT user_id, email, password_hash, display_name, status, created_at, updated_at
FROM users
WHERE email = $1
LIMIT 1;

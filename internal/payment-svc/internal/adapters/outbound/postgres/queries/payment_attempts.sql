-- name: CreateInitiatedPaymentAttempt :one
INSERT INTO
  payment_attempts (
    order_id,
    status,
    amount,
    currency,
    provider_name,
    idempotency_key
  )
VALUES
  (
    sqlc.arg (order_id),
    sqlc.arg (status),
    sqlc.arg (amount),
    sqlc.arg (currency),
    sqlc.arg (provider_name),
    sqlc.arg (idempotency_key)
  )
RETURNING
  *;

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

-- name: GetPaymentAttemptByOrderIDAndIdempotencyKey :one
SELECT
  *
FROM
  payment_attempts
WHERE
  order_id = sqlc.arg (order_id)
  AND idempotency_key = sqlc.arg (idempotency_key)
LIMIT
  1;

-- name: MarkPaymentAttemptProcessing :one
UPDATE payment_attempts
SET
  status = sqlc.arg (status),
  failure_code = NULL,
  failure_message = NULL,
  updated_at = now()
WHERE
  payment_attempt_id = sqlc.arg (payment_attempt_id)
  AND status = 'initiated'
RETURNING
  *;

-- name: MarkPaymentAttemptSucceeded :one
UPDATE payment_attempts
SET
  status = sqlc.arg (status),
  provider_reference = sqlc.arg (provider_reference),
  failure_code = NULL,
  failure_message = NULL,
  updated_at = now()
WHERE
  payment_attempt_id = sqlc.arg (payment_attempt_id)
  AND status = 'processing'
RETURNING
  *;

-- name: MarkPaymentAttemptFailed :one
UPDATE payment_attempts
SET
  status = sqlc.arg (status),
  failure_code = sqlc.arg (failure_code),
  failure_message = sqlc.arg (failure_message),
  updated_at = now()
WHERE
  payment_attempt_id = sqlc.arg (payment_attempt_id)
  AND status = 'processing'
RETURNING
  *;

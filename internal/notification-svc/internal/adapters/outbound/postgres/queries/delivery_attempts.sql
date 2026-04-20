-- name: CreateDeliveryAttempt :one
INSERT INTO
  delivery_attempts (
    delivery_request_id,
    attempt_number,
    provider_name,
    provider_message_id,
    failure_code,
    failure_message,
    attempted_at
  )
VALUES
  (
    sqlc.arg (delivery_request_id),
    sqlc.arg (attempt_number),
    sqlc.arg (provider_name),
    sqlc.arg (provider_message_id),
    sqlc.narg (failure_code),
    sqlc.narg (failure_message),
    sqlc.arg (attempted_at)
  )
RETURNING
  *;

-- name: ListDeliveryAttemptsByDeliveryRequestID :many
SELECT
  *
FROM
  delivery_attempts
WHERE
  delivery_request_id = sqlc.arg (delivery_request_id)
ORDER BY
  attempt_number ASC;

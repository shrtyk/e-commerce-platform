-- name: CreateRequestedDeliveryRequest :one
INSERT INTO
  delivery_requests (
    source_event_id,
    source_event_name,
    channel,
    recipient,
    template_key,
    status,
    idempotency_key
  )
VALUES
  (
    sqlc.arg (source_event_id),
    sqlc.arg (source_event_name),
    sqlc.arg (channel),
    sqlc.arg (recipient),
    sqlc.arg (template_key),
    sqlc.arg (status),
    sqlc.arg (idempotency_key)
  )
RETURNING
  *;

-- name: GetDeliveryRequestByID :one
SELECT
  *
FROM
  delivery_requests
WHERE
  delivery_request_id = sqlc.arg (delivery_request_id)
LIMIT
  1;

-- name: GetDeliveryRequestByIdempotencyKey :one
SELECT
  *
FROM
  delivery_requests
WHERE
  idempotency_key = sqlc.arg (idempotency_key)
LIMIT
  1;

-- name: MarkDeliveryRequestSent :one
UPDATE delivery_requests
SET
  status = sqlc.arg (status),
  last_error_code = NULL,
  last_error_message = NULL,
  updated_at = now()
WHERE
  delivery_request_id = sqlc.arg (delivery_request_id)
RETURNING
  *;

-- name: MarkDeliveryRequestFailed :one
UPDATE delivery_requests
SET
  status = sqlc.arg (status),
  last_error_code = sqlc.arg (last_error_code),
  last_error_message = sqlc.arg (last_error_message),
  updated_at = now()
WHERE
  delivery_request_id = sqlc.arg (delivery_request_id)
RETURNING
  *;

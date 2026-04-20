-- name: CreateConsumerIdempotency :exec
INSERT INTO
  consumer_idempotency (event_id, consumer_group_name, delivery_request_id)
VALUES
  (
    sqlc.arg (event_id),
    sqlc.arg (consumer_group_name),
    sqlc.arg (delivery_request_id)
  );

-- name: ConsumerIdempotencyExists :one
SELECT
  EXISTS (
    SELECT
      1
    FROM
      consumer_idempotency
    WHERE
      event_id = sqlc.arg (event_id)
      AND consumer_group_name = sqlc.arg (consumer_group_name)
  );

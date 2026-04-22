-- name: CreateStockRecord :one
INSERT INTO
  stock_records (product_id, quantity, reserved)
VALUES
  (
    sqlc.arg (product_id),
    sqlc.arg (quantity),
    sqlc.arg (reserved)
  )
RETURNING
  *;

-- name: GetStockRecordByProductID :one
SELECT
  *
FROM
  stock_records
WHERE
  product_id = sqlc.arg (product_id);

-- name: GetStockRecordByProductIDForUpdate :one
SELECT
  *
FROM
  stock_records
WHERE
  product_id = sqlc.arg (product_id)
FOR UPDATE;

-- name: UpdateStockRecord :one
UPDATE stock_records
SET
  quantity = sqlc.arg (quantity),
  reserved = sqlc.arg (reserved),
  updated_at = NOW()
WHERE
  stock_record_id = sqlc.arg (stock_record_id)
RETURNING
  *;

-- name: CreateStockReservation :one
INSERT INTO
  stock_reservations (order_id, product_id, quantity)
VALUES
  (
    sqlc.arg (order_id),
    sqlc.arg (product_id),
    sqlc.arg (quantity)
  )
RETURNING
  *;

-- name: ListStockReservationsByOrderID :many
SELECT
  *
FROM
  stock_reservations
WHERE
  order_id = sqlc.arg (order_id)
ORDER BY
  product_id;

-- name: DeleteStockReservationsByOrderID :exec
DELETE FROM stock_reservations
WHERE
  order_id = sqlc.arg (order_id);

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

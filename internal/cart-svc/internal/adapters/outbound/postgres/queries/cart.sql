-- name: GetActiveCartByUserID :one
SELECT
  *
FROM
  carts
WHERE
  user_id = sqlc.arg (user_id)
  AND status = 'active';

-- name: CreateActiveCart :one
INSERT INTO
  carts (user_id, status, currency)
VALUES
  (sqlc.arg (user_id), 'active', sqlc.arg (currency))
RETURNING
  *;

-- name: ListCartItemsByCartID :many
SELECT
  *
FROM
  cart_items
WHERE
  cart_id = sqlc.arg (cart_id)
ORDER BY
  created_at ASC;

-- name: InsertCartItem :one
INSERT INTO
  cart_items (cart_id, sku, quantity, unit_price, currency, product_name)
VALUES
  (
    sqlc.arg (cart_id),
    sqlc.arg (sku),
    sqlc.arg (quantity),
    sqlc.arg (unit_price),
    sqlc.arg (currency),
    sqlc.arg (product_name)
  )
ON CONFLICT (cart_id, sku) DO UPDATE
SET
  quantity = cart_items.quantity + EXCLUDED.quantity,
  updated_at = NOW()
RETURNING
  *;

-- name: UpdateCartItemQuantity :one
UPDATE cart_items
SET
  quantity = sqlc.arg (quantity),
  updated_at = NOW()
WHERE
  cart_id = sqlc.arg (cart_id)
  AND sku = sqlc.arg (sku)
RETURNING
  *;

-- name: DeleteCartItem :execrows
DELETE FROM cart_items
WHERE
  cart_id = sqlc.arg (cart_id)
  AND sku = sqlc.arg (sku);

-- name: GetProductSnapshotBySKU :one
SELECT
  *
FROM
  product_snapshots
WHERE
  sku = sqlc.arg (sku);

-- name: UpsertProductSnapshot :one
INSERT INTO
  product_snapshots (sku, product_id, name, unit_price, currency)
VALUES
  (
    sqlc.arg (sku),
    sqlc.narg (product_id),
    sqlc.arg (name),
    sqlc.arg (unit_price),
    sqlc.arg (currency)
  )
ON CONFLICT (sku) DO UPDATE
SET
  product_id = EXCLUDED.product_id,
  name = EXCLUDED.name,
  unit_price = EXCLUDED.unit_price,
  currency = EXCLUDED.currency,
  updated_at = NOW()
RETURNING
  *;

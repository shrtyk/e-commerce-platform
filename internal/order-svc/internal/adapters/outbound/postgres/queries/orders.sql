-- name: CreateOrder :one
INSERT INTO
  orders (order_id, user_id, status, currency, total_amount)
VALUES
  (
    sqlc.arg (order_id),
    sqlc.arg (user_id),
    sqlc.arg (status),
    sqlc.arg (currency),
    sqlc.arg (total_amount)
  )
RETURNING
  *;

-- name: CreateOrderItem :one
INSERT INTO
  order_items (
    order_id,
    product_id,
    sku,
    name,
    quantity,
    unit_price,
    line_total,
    currency
  )
VALUES
  (
    sqlc.arg (order_id),
    sqlc.arg (product_id),
    sqlc.arg (sku),
    sqlc.arg (name),
    sqlc.arg (quantity),
    sqlc.arg (unit_price),
    sqlc.arg (line_total),
    sqlc.arg (currency)
  )
RETURNING
  *;

-- name: CreateOrderSagaState :one
INSERT INTO
  order_saga_state (order_id, stock_stage, payment_stage, last_error_code)
VALUES
  (
    sqlc.arg (order_id),
    sqlc.arg (stock_stage),
    sqlc.arg (payment_stage),
    sqlc.narg (last_error_code)
  )
RETURNING
  *;

-- name: CreateOrderCheckoutIdempotency :exec
INSERT INTO
  order_checkout_idempotency (order_id, user_id, idempotency_key)
VALUES
  (
    sqlc.arg (order_id),
    sqlc.arg (user_id),
    sqlc.arg (idempotency_key)
  );

-- name: GetOrderByID :one
SELECT
  *
FROM
  orders
WHERE
  order_id = sqlc.arg (order_id);

-- name: GetOrderSagaStateByOrderID :one
SELECT
  *
FROM
  order_saga_state
WHERE
  order_id = sqlc.arg (order_id);

-- name: ListOrderItemsByOrderID :many
SELECT
  *
FROM
  order_items
WHERE
  order_id = sqlc.arg (order_id)
ORDER BY
  created_at ASC;

-- name: GetOrderByUserIDAndIdempotencyKey :one
SELECT
  o.*
FROM
  order_checkout_idempotency i
  INNER JOIN orders o ON o.order_id = i.order_id
WHERE
  i.user_id = sqlc.arg (user_id)
  AND i.idempotency_key = sqlc.arg (idempotency_key);

-- name: AppendOrderStatusHistory :one
INSERT INTO
  order_status_history (order_id, from_status, to_status, reason_code)
VALUES
  (
    sqlc.arg (order_id),
    sqlc.narg (from_status),
    sqlc.arg (to_status),
    sqlc.narg (reason_code)
  )
RETURNING
  *;

-- name: MarkOrderSagaStockRequested :one
UPDATE order_saga_state
SET
  stock_stage = 'requested',
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
  AND stock_stage IN ('not_started', 'requested')
RETURNING
  *;

-- name: MarkOrderSagaStockSucceeded :one
UPDATE order_saga_state
SET
  stock_stage = 'succeeded',
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
  AND stock_stage IN ('requested', 'succeeded')
RETURNING
  *;

-- name: MarkOrderSagaStockFailed :one
UPDATE order_saga_state
SET
  stock_stage = 'failed',
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
  AND stock_stage IN ('requested', 'failed')
RETURNING
  *;

-- name: MarkOrderSagaPaymentRequested :one
UPDATE order_saga_state
SET
  payment_stage = 'requested',
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
  AND payment_stage IN ('not_started', 'requested')
RETURNING
  *;

-- name: MarkOrderSagaPaymentSucceeded :one
UPDATE order_saga_state
SET
  payment_stage = 'succeeded',
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
  AND payment_stage IN ('requested', 'succeeded')
RETURNING
  *;

-- name: MarkOrderSagaPaymentFailed :one
UPDATE order_saga_state
SET
  payment_stage = 'failed',
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
  AND payment_stage IN ('requested', 'failed')
RETURNING
  *;

-- name: SetOrderSagaLastErrorCode :one
UPDATE order_saga_state
SET
  last_error_code = sqlc.arg (last_error_code),
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
RETURNING
  *;

-- name: ClearOrderSagaLastErrorCode :one
UPDATE order_saga_state
SET
  last_error_code = NULL,
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
RETURNING
  *;

-- name: TransitionOrderStatus :one
UPDATE orders
SET
  status = sqlc.arg (to_status),
  updated_at = NOW()
WHERE
  order_id = sqlc.arg (order_id)
  AND status = sqlc.arg (from_status)
RETURNING
  *;

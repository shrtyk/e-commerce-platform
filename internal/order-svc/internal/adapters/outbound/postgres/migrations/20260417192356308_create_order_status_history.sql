-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS order_status_history (
  order_status_history_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders (order_id) ON DELETE CASCADE,
  from_status TEXT,
  to_status TEXT NOT NULL,
  reason_code TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    from_status IS NULL
    OR from_status IN (
      'pending',
      'awaiting_stock',
      'awaiting_payment',
      'confirmed',
      'cancelled'
    )
  ),
  CHECK (
    to_status IN (
      'pending',
      'awaiting_stock',
      'awaiting_payment',
      'confirmed',
      'cancelled'
    )
  )
);

CREATE INDEX IF NOT EXISTS idx_order_status_history_order_id ON order_status_history (order_id, created_at DESC);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_order_status_history_order_id;

DROP TABLE IF EXISTS order_status_history;

-- +goose StatementEnd

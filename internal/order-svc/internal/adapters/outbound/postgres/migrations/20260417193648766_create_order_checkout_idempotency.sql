-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS order_checkout_idempotency (
  order_checkout_idempotency_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders (order_id) ON DELETE CASCADE,
  user_id UUID NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_fingerprint TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (idempotency_key <> ''),
  CHECK (payload_fingerprint <> ''),
  CONSTRAINT uq_order_checkout_idempotency_user_key UNIQUE (user_id, idempotency_key),
  CONSTRAINT uq_order_checkout_idempotency_order_id UNIQUE (order_id)
);

CREATE INDEX IF NOT EXISTS idx_order_checkout_idempotency_order_id ON order_checkout_idempotency (order_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_order_checkout_idempotency_order_id;

DROP TABLE IF EXISTS order_checkout_idempotency;

-- +goose StatementEnd

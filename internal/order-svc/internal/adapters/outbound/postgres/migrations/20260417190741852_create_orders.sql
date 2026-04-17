-- +goose Up
-- +goose StatementBegin
CREATE TYPE order_status AS ENUM(
  'pending',
  'awaiting_stock',
  'awaiting_payment',
  'confirmed',
  'cancelled'
);

CREATE TABLE IF NOT EXISTS orders (
  order_id UUID PRIMARY KEY,
  user_id UUID NOT NULL,
  status order_status NOT NULL,
  currency TEXT NOT NULL,
  total_amount BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    currency = UPPER(BTRIM(currency))
    AND CHAR_LENGTH(BTRIM(currency)) = 3
  ),
  CHECK (total_amount >= 0)
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS orders;

DROP TYPE IF EXISTS order_status;

-- +goose StatementEnd

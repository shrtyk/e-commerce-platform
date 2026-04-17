-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS order_items (
  order_item_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders (order_id) ON DELETE CASCADE,
  product_id UUID NOT NULL,
  sku TEXT NOT NULL,
  name TEXT NOT NULL,
  quantity INTEGER NOT NULL,
  unit_price BIGINT NOT NULL,
  line_total BIGINT NOT NULL,
  currency TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (sku <> ''),
  CHECK (name <> ''),
  CHECK (quantity > 0),
  CHECK (unit_price >= 0),
  CHECK (line_total >= 0),
  CHECK (line_total = quantity::BIGINT * unit_price),
  CHECK (
    currency = UPPER(BTRIM(currency))
    AND CHAR_LENGTH(BTRIM(currency)) = 3
  )
);

CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items (order_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_order_items_order_id;

DROP TABLE IF EXISTS order_items;

-- +goose StatementEnd

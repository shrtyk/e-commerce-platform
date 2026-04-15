-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS product_snapshots (
  sku TEXT PRIMARY KEY,
  product_id UUID,
  name TEXT NOT NULL,
  unit_price BIGINT NOT NULL,
  currency TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (sku <> ''),
  CHECK (name <> ''),
  CHECK (unit_price >= 0),
  CHECK (currency <> ''),
  CONSTRAINT uq_product_snapshots_product_id UNIQUE (product_id)
);

CREATE TABLE IF NOT EXISTS carts (
  cart_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  user_id UUID NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  currency TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (status IN ('active', 'checked_out', 'expired')),
  CHECK (currency <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_carts_user_active ON carts (user_id)
WHERE
  status = 'active';

CREATE TABLE IF NOT EXISTS cart_items (
  cart_item_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  cart_id UUID NOT NULL REFERENCES carts (cart_id) ON DELETE CASCADE,
  sku TEXT NOT NULL REFERENCES product_snapshots (sku) ON DELETE RESTRICT,
  quantity INTEGER NOT NULL,
  unit_price BIGINT NOT NULL,
  currency TEXT NOT NULL,
  product_name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (quantity > 0),
  CHECK (unit_price >= 0),
  CHECK (currency <> ''),
  CHECK (product_name <> ''),
  CONSTRAINT uq_cart_items_cart_sku UNIQUE (cart_id, sku)
);

CREATE INDEX IF NOT EXISTS idx_cart_items_cart_id ON cart_items (cart_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_cart_items_cart_id;

DROP TABLE IF EXISTS cart_items;

DROP INDEX IF EXISTS uq_carts_user_active;

DROP TABLE IF EXISTS carts;

DROP TABLE IF EXISTS product_snapshots;

-- +goose StatementEnd

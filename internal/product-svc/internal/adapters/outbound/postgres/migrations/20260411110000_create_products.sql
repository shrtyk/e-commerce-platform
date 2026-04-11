-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS products (
  product_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  sku TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT,
  price BIGINT NOT NULL,
  currency_id UUID NOT NULL REFERENCES currencies (id) ON DELETE RESTRICT,
  category_id UUID,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (sku <> ''),
  CHECK (name <> ''),
  CHECK (price >= 0),
  CHECK (status IN ('draft', 'published', 'archived'))
);

CREATE INDEX IF NOT EXISTS idx_products_currency_id ON products (currency_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_products_currency_id;

DROP TABLE IF EXISTS products;

-- +goose StatementEnd

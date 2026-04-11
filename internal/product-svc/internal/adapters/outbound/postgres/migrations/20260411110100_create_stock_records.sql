-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stock_records (
  stock_record_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  product_id UUID NOT NULL,
  quantity INTEGER NOT NULL,
  reserved INTEGER NOT NULL DEFAULT 0,
  available INTEGER GENERATED ALWAYS AS (quantity - reserved) STORED,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT fk_stock_records_product FOREIGN KEY (product_id) REFERENCES products (product_id) ON DELETE CASCADE,
  CHECK (quantity >= 0),
  CHECK (reserved >= 0),
  CHECK (reserved <= quantity)
);

CREATE INDEX IF NOT EXISTS idx_stock_records_product_id ON stock_records (product_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_stock_records_product_id;

DROP TABLE IF EXISTS stock_records;

-- +goose StatementEnd

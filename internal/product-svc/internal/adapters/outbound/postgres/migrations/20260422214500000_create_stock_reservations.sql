-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stock_reservations (
  stock_reservation_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  order_id UUID NOT NULL,
  product_id UUID NOT NULL,
  quantity INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT fk_stock_reservations_product FOREIGN KEY (product_id) REFERENCES products (product_id) ON DELETE CASCADE,
  CONSTRAINT uq_stock_reservations_order_product UNIQUE (order_id, product_id),
  CHECK (quantity > 0)
);

CREATE INDEX IF NOT EXISTS idx_stock_reservations_order_id ON stock_reservations (order_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_stock_reservations_order_id;

DROP TABLE IF EXISTS stock_reservations;

-- +goose StatementEnd

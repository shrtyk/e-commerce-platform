-- +goose Up
-- +goose StatementBegin
CREATE TYPE order_saga_stage AS ENUM('not_started', 'requested', 'succeeded', 'failed');

CREATE TABLE IF NOT EXISTS order_saga_state (
  order_id UUID PRIMARY KEY REFERENCES orders (order_id) ON DELETE CASCADE,
  stock_stage order_saga_stage NOT NULL DEFAULT 'not_started',
  payment_stage order_saga_stage NOT NULL DEFAULT 'not_started',
  last_error_code TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS order_saga_state;

DROP TYPE IF EXISTS order_saga_stage;

-- +goose StatementEnd

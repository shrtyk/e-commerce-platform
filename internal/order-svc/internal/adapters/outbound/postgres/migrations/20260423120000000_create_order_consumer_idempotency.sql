-- +goose Up
CREATE TABLE order_consumer_idempotency (
  event_id UUID NOT NULL,
  consumer_group_name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (event_id, consumer_group_name)
);

-- +goose Down
DROP TABLE IF EXISTS order_consumer_idempotency;

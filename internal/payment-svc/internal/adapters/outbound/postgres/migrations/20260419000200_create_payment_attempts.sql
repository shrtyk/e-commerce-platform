-- +goose Up
-- +goose StatementBegin
CREATE TYPE payment_status AS ENUM(
  'initiated',
  'processing',
  'succeeded',
  'failed'
);

CREATE TABLE IF NOT EXISTS payment_attempts (
  payment_attempt_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  order_id UUID NOT NULL,
  status payment_status NOT NULL,
  amount BIGINT NOT NULL,
  currency TEXT NOT NULL,
  provider_name TEXT NOT NULL,
  provider_reference TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    currency = UPPER(BTRIM(currency))
    AND CHAR_LENGTH(BTRIM(currency)) = 3
  ),
  CHECK (amount > 0),
  CHECK (provider_name <> ''),
  CHECK (idempotency_key <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_attempts_order_idempotency ON payment_attempts (order_id, idempotency_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_payment_attempts_order_idempotency;

DROP TABLE IF EXISTS payment_attempts;

DROP TYPE IF EXISTS payment_status;
-- +goose StatementEnd

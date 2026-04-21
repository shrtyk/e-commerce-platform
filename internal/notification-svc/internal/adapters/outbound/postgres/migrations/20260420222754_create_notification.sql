-- +goose Up
-- +goose StatementBegin
CREATE TYPE delivery_status AS ENUM('requested', 'sent', 'failed');

CREATE TABLE IF NOT EXISTS delivery_requests (
  delivery_request_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  source_event_id UUID NOT NULL,
  correlation_id TEXT NOT NULL,
  source_event_name TEXT NOT NULL,
  channel TEXT NOT NULL,
  recipient TEXT NOT NULL,
  template_key TEXT NOT NULL,
  status delivery_status NOT NULL,
  idempotency_key TEXT NOT NULL,
  last_error_code TEXT,
  last_error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uq_delivery_requests_idempotency_key UNIQUE (idempotency_key),
  CHECK (BTRIM(source_event_name) <> ''),
  CHECK (BTRIM(correlation_id) <> ''),
  CHECK (BTRIM(channel) <> ''),
  CHECK (BTRIM(recipient) <> ''),
  CHECK (BTRIM(template_key) <> ''),
  CHECK (BTRIM(idempotency_key) <> ''),
  CHECK (
    (
      status = 'failed'
      AND COALESCE(BTRIM(last_error_code), '') <> ''
      AND COALESCE(BTRIM(last_error_message), '') <> ''
    )
    OR (
      status <> 'failed'
      AND last_error_code IS NULL
      AND last_error_message IS NULL
    )
  )
);

CREATE INDEX IF NOT EXISTS idx_delivery_requests_source_event_id ON delivery_requests (source_event_id);

CREATE INDEX IF NOT EXISTS idx_delivery_requests_status ON delivery_requests (status);

CREATE TABLE IF NOT EXISTS delivery_attempts (
  delivery_attempt_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  delivery_request_id UUID NOT NULL REFERENCES delivery_requests (delivery_request_id) ON DELETE CASCADE,
  attempt_number INTEGER NOT NULL,
  provider_name TEXT NOT NULL,
  provider_message_id TEXT NOT NULL,
  failure_code TEXT,
  failure_message TEXT,
  attempted_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uq_delivery_attempts_request_attempt UNIQUE (delivery_request_id, attempt_number),
  CHECK (attempt_number > 0),
  CHECK (BTRIM(provider_name) <> ''),
  CHECK (BTRIM(provider_message_id) <> ''),
  CHECK (
    (
      COALESCE(BTRIM(failure_code), '') = ''
      AND COALESCE(BTRIM(failure_message), '') = ''
    )
    OR (
      COALESCE(BTRIM(failure_code), '') <> ''
      AND COALESCE(BTRIM(failure_message), '') <> ''
    )
  )
);

CREATE TABLE IF NOT EXISTS consumer_idempotency (
  event_id UUID NOT NULL,
  consumer_group_name TEXT NOT NULL,
  delivery_request_id UUID NOT NULL REFERENCES delivery_requests (delivery_request_id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, consumer_group_name),
  CHECK (BTRIM(consumer_group_name) <> '')
);

CREATE INDEX IF NOT EXISTS idx_consumer_idempotency_delivery_request_id ON consumer_idempotency (delivery_request_id);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_consumer_idempotency_delivery_request_id;

DROP TABLE IF EXISTS consumer_idempotency;

DROP TABLE IF EXISTS delivery_attempts;

DROP INDEX IF EXISTS idx_delivery_requests_status;

DROP INDEX IF EXISTS idx_delivery_requests_source_event_id;

DROP TABLE IF EXISTS delivery_requests;

DROP TYPE IF EXISTS delivery_status;

-- +goose StatementEnd

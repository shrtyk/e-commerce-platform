-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS outbox_records (
  id UUID PRIMARY KEY DEFAULT uuidv7 (),
  event_id TEXT NOT NULL,
  event_name TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  key BYTEA,
  payload BYTEA NOT NULL,
  headers JSONB NOT NULL DEFAULT '{}'::jsonb,
  attempt INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  last_error TEXT NOT NULL DEFAULT '',
  next_attempt_at TIMESTAMPTZ,
  locked_at TIMESTAMPTZ,
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (event_id <> ''),
  CHECK (event_name <> ''),
  CHECK (aggregate_type <> ''),
  CHECK (aggregate_id <> ''),
  CHECK (topic <> ''),
  CHECK (octet_length(payload) > 0),
  CHECK (attempt >= 0),
  CHECK (
    status IN ('pending', 'in_progress', 'published', 'failed')
  )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_records_event_id ON outbox_records (event_id);

CREATE INDEX IF NOT EXISTS idx_outbox_records_claim ON outbox_records (status, next_attempt_at, created_at);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_outbox_records_claim;

DROP INDEX IF EXISTS idx_outbox_records_event_id;

DROP TABLE IF EXISTS outbox_records;

-- +goose StatementEnd

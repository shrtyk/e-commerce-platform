-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT
      1
    FROM
      pg_type
    WHERE
      typname = 'outbox_status'
  ) THEN
    CREATE TYPE outbox_status AS ENUM ('pending', 'in_progress', 'published', 'dead');
  END IF;
END
$$;

CREATE TABLE IF NOT EXISTS outbox_records (
  id UUID PRIMARY KEY DEFAULT uuidv7 (),
  event_id UUID NOT NULL,
  event_name TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  key BYTEA,
  payload BYTEA NOT NULL,
  headers JSONB NOT NULL DEFAULT '{}'::jsonb,
  attempt INTEGER NOT NULL DEFAULT 0,
  status outbox_status NOT NULL DEFAULT 'pending',
  last_error TEXT,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  locked_at TIMESTAMPTZ,
  locked_by TEXT,
  published_at TIMESTAMPTZ,
  max_attempts INTEGER NOT NULL DEFAULT 20,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (event_name <> ''),
  CHECK (aggregate_type <> ''),
  CHECK (aggregate_id <> ''),
  CHECK (topic <> ''),
  CHECK (octet_length(payload) > 0),
  CHECK (attempt >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_records_event_id ON outbox_records (event_id);

CREATE INDEX IF NOT EXISTS idx_outbox_records_ready_pending ON outbox_records (next_attempt_at, created_at, id)
WHERE
  status = 'pending'::outbox_status;

CREATE INDEX IF NOT EXISTS idx_outbox_records_stale_in_progress ON outbox_records (locked_at, created_at, id)
WHERE
  status = 'in_progress'::outbox_status;

CREATE INDEX IF NOT EXISTS idx_outbox_records_published_cleanup ON outbox_records (published_at, id)
WHERE
  status = 'published'::outbox_status;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_outbox_records_published_cleanup;

DROP INDEX IF EXISTS idx_outbox_records_stale_in_progress;

DROP INDEX IF EXISTS idx_outbox_records_ready_pending;

DROP INDEX IF EXISTS idx_outbox_records_event_id;

DROP TABLE IF EXISTS outbox_records;

DROP TYPE IF EXISTS outbox_status;

-- +goose StatementEnd

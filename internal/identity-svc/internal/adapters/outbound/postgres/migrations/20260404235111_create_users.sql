-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS users (
  user_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name TEXT,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    email <> ''
    AND char_length(email) <= 254
  ),
  CHECK (
    password_hash <> ''
    AND char_length(password_hash) <= 255
  ),
  CHECK (
    display_name IS NULL
    OR display_name <> ''
  ),
  CHECK (
    display_name IS NULL
    OR char_length(display_name) <= 100
  ),
  CHECK (status IN ('active', 'disabled'))
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS users;

-- +goose StatementEnd

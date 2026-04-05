-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS roles (
  role_code TEXT PRIMARY KEY,
  CHECK (role_code IN ('user', 'admin'))
);

INSERT INTO
  roles (role_code)
VALUES
  ('user'),
  ('admin')
ON CONFLICT (role_code) DO NOTHING;

CREATE TABLE IF NOT EXISTS users (
  user_id UUID PRIMARY KEY DEFAULT uuidv7 (),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name TEXT,
  role_code TEXT NOT NULL DEFAULT 'user',
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT fk_users_role FOREIGN KEY (role_code) REFERENCES roles (role_code),
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
  CHECK (role_code IN ('user', 'admin')),
  CHECK (status IN ('active', 'disabled'))
);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS users;

DROP TABLE IF EXISTS roles;

-- +goose StatementEnd

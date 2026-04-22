-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS currencies (
  id UUID PRIMARY KEY DEFAULT uuidv7 (),
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  symbol TEXT,
  decimals INT NOT NULL DEFAULT 2,
  CHECK (code <> ''),
  CHECK (name <> ''),
  CHECK (decimals >= 0)
);

INSERT INTO
  currencies (code, name, symbol, decimals)
VALUES
  ('USD', 'US Dollar', '$', 2),
  ('EUR', 'Euro', '€', 2),
  ('RUB', 'Russian Ruble', '₽', 2)
ON CONFLICT (code) DO NOTHING;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS currencies;

-- +goose StatementEnd

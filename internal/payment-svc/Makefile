.PHONY: run unit-tests integration-tests migrate-up migrate-create sqlc-gen mock-gen

MIGRATIONS_DIR := internal/adapters/outbound/postgres/migrations
DB_DSN := postgres://$${PAYMENT_POSTGRES_USER:-payment}:$${PAYMENT_POSTGRES_PASSWORD:-payment}@localhost:$${PAYMENT_POSTGRES_PORT:-5437}/$${PAYMENT_POSTGRES_DB:-payment}?sslmode=disable
SQLC_CONFIG := internal/adapters/outbound/postgres/sqlc.yaml

run:
	@test -f ".env" || { printf "Missing env file: .env\n"; exit 1; }
	@set -a; . .env; set +a; GOWORK=off go run ./cmd/app

unit-tests:
	@go test -v ./...

integration-tests:
	@printf "payment-svc integration tests are not implemented; skipping.\n"

migrate-up:
	@goose -dir "$(MIGRATIONS_DIR)" postgres "$(DB_DSN)" up

migrate-create:
	@test -n "$(name)" || { printf "Usage: make migrate-create name=<migration_name>\n"; exit 1; }
	@goose -dir "$(MIGRATIONS_DIR)" create "$(name)" sql

sqlc-gen:
	@sqlc generate -f "$(SQLC_CONFIG)"

mock-gen:
	@mockery --config tools/.mockery.yaml

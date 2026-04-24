.PHONY: run unit-tests integration-tests migrate-up migrate-create sqlc-gen mock-gen

MIGRATIONS_DIR := internal/adapters/outbound/postgres/migrations
DB_DSN := postgres://$${NOTIFICATION_POSTGRES_USER:-notification}:$${NOTIFICATION_POSTGRES_PASSWORD:-notification}@localhost:$${NOTIFICATION_POSTGRES_PORT:-5438}/$${NOTIFICATION_POSTGRES_DB:-notification}?sslmode=disable
SQLC_CONFIG := internal/adapters/outbound/postgres/sqlc.yaml

run:
	@test -f ".env" || { printf "Missing env file: .env\n"; exit 1; }
	@set -a; . .env; set +a; GOWORK=off go run ./cmd/app

unit-tests:
	@if [ -n "$$(go list ./... 2>/dev/null)" ]; then go test -v ./...; else printf "notification-svc has no Go packages; skipping unit tests.\n"; fi

integration-tests:
	@go test -v -tags=integration ./internal/integration/... -count=1

migrate-up:
	@goose -dir "$(MIGRATIONS_DIR)" postgres "$(DB_DSN)" up

migrate-create:
	@test -n "$(name)" || { printf "Usage: make migrate-create name=<migration_name>\n"; exit 1; }
	@goose -dir "$(MIGRATIONS_DIR)" create "$(name)" sql

sqlc-gen:
	@sqlc generate -f "$(SQLC_CONFIG)"

mock-gen:
	@mockery --config tools/.mockery.yaml

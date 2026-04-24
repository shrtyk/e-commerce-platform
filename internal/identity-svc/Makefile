.PHONY: run unit-tests integration-tests migrate-up migrate-create sqlc-gen mock-gen openapi-gen-dto

MIGRATIONS_DIR := internal/adapters/outbound/postgres/migrations
DB_DSN := postgres://$${IDENTITY_POSTGRES_USER:-identity}:$${IDENTITY_POSTGRES_PASSWORD:-identity}@localhost:$${IDENTITY_POSTGRES_PORT:-5433}/$${IDENTITY_POSTGRES_DB:-identity}?sslmode=disable
SQLC_CONFIG := internal/adapters/outbound/postgres/sqlc.yaml
ROOT_DIR ?= ../..
OPENAPI_FILE := api/openapi/public/openapi.yaml
OAPI_CODEGEN := oapi-codegen

run:
	@test -f ".env" || { printf "Missing env file: .env\n"; exit 1; }
	@set -a; . .env; set +a; GOWORK=off go run ./cmd/app

unit-tests:
	@go test -v ./...

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

openapi-gen-dto:
	@mkdir -p internal/adapters/inbound/http/dto
	@cd "$(ROOT_DIR)" && $(OAPI_CODEGEN) -config internal/identity-svc/tools/oapi-codegen.yaml "$(OPENAPI_FILE)"

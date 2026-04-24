.PHONY: run unit-tests integration-tests migrate-up sqlc-gen mock-gen openapi-gen-dto

MIGRATIONS_DIR := internal/adapters/outbound/postgres/migrations
DB_DSN := postgres://$${CATALOG_POSTGRES_USER:-catalog}:$${CATALOG_POSTGRES_PASSWORD:-catalog}@localhost:$${CATALOG_POSTGRES_PORT:-5434}/$${CATALOG_POSTGRES_DB:-catalog}?sslmode=disable
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

sqlc-gen:
	@sqlc generate -f "$(SQLC_CONFIG)"

mock-gen:
	@mockery --config tools/.mockery.yaml

openapi-gen-dto:
	@mkdir -p internal/adapters/inbound/http/dto
	@cd "$(ROOT_DIR)" && $(OAPI_CODEGEN) -config internal/product-svc/tools/oapi-codegen.yaml "$(OPENAPI_FILE)"

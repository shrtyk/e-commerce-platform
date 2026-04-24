.PHONY: run unit-tests integration-tests migrate-up sqlc-gen mock-gen openapi-gen-dto

MIGRATIONS_DIR := internal/adapters/outbound/postgres/migrations
DB_DSN := postgres://$${CART_POSTGRES_USER:-cart}:$${CART_POSTGRES_PASSWORD:-cart}@localhost:$${CART_POSTGRES_PORT:-5435}/$${CART_POSTGRES_DB:-cart}?sslmode=disable
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
	@printf "cart-svc has no .mockery.yaml; skipping mock generation.\n"

openapi-gen-dto:
	@mkdir -p internal/adapters/inbound/http/dto
	@cd "$(ROOT_DIR)" && $(OAPI_CODEGEN) -config internal/cart-svc/tools/oapi-codegen.yaml "$(OPENAPI_FILE)"

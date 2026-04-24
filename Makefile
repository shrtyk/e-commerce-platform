# tools
REDOCLY := $(shell command -v redocly 2>/dev/null || printf "npx @redocly/cli")

# generate contracts/mocks
PROTO_SRC := api/proto
PROTO_GO_MODULE := github.com/shrtyk/e-commerce-platform
PROTO_FILES := $(PROTO_SRC)/common/v1/*.proto $(PROTO_SRC)/identity/v1/*.proto $(PROTO_SRC)/catalog/v1/*.proto $(PROTO_SRC)/cart/v1/*.proto $(PROTO_SRC)/order/v1/*.proto $(PROTO_SRC)/payment/v1/*.proto $(PROTO_SRC)/notification/v1/*.proto
OPENAPI_FILE := api/openapi/public/openapi.yaml
ASYNCAPI_FILE := api/asyncapi/kafka-events.yaml
SERVICES := identity product cart order payment notification
MIGRATION_SERVICES := $(SERVICES)
MOCK_GEN_SERVICES := $(SERVICES)
SQLC_GEN_SERVICES := identity order
OPENAPI_DTO_SERVICES := identity product cart order
OPENAPI_DTO_TARGETS := $(addprefix openapi-gen-,$(addsuffix -dto,$(OPENAPI_DTO_SERVICES)))

# compose
COMPOSE := docker compose -p ecom
COMPOSE_FILES := -f deploy/compose/platform.yaml -f deploy/compose/identity.yaml -f deploy/compose/catalog.yaml -f deploy/compose/cart.yaml -f deploy/compose/order.yaml -f deploy/compose/payment.yaml -f deploy/compose/notification.yaml -f deploy/compose/observability.yaml
COMPOSE_INFRA_SERVICES := kafka kafka-topics-init schema-registry
COMPOSE_DATA_SERVICES := identity-postgres catalog-postgres cart-postgres cart-redis order-postgres payment-postgres notification-postgres
COMPOSE_APP_SERVICES := identity-svc product-svc cart-svc order-svc payment-svc notification-svc gateway
COMPOSE_MIGRATION_SERVICES := identity-migrate product-migrate cart-migrate order-migrate payment-migrate notification-migrate
COMPOSE_OBSERVABILITY_SERVICES := prometheus loki tempo grafana otel-collector
COMPOSE_SERVICES := $(COMPOSE_INFRA_SERVICES) $(COMPOSE_DATA_SERVICES) $(COMPOSE_APP_SERVICES) $(COMPOSE_MIGRATION_SERVICES) $(COMPOSE_OBSERVABILITY_SERVICES)
CHECKOUT_HOST_SERVICE_PORTS := 18081 18082 18083 18084 18085 18086
CHECKOUT_HOST_SERVICE_LOG_DIR := .local/logs/checkout-host-services

.PHONY: help tools-check tools-install mock-gen proto-check proto-gen-common proto-gen-identity proto-gen-catalog proto-gen-cart proto-gen-order proto-gen-payment proto-gen-notification proto-gen sqlc-gen openapi-lint asyncapi-lint openapi-gen-dto contracts compose-config compose-up compose-up-data compose-up-shared compose-up-observability compose-down compose-logs compose-logs-data compose-logs-shared compose-logs-observability compose-ps migrate-up-checkout-local unit-tests unit-tests-common integration-tests e2e-checkout-up e2e-checkout-down e2e-tests

help:
	@printf "Targets:\n"
	@printf "\nTools\n"
	@printf "  tools-check              Verify required tools\n"
	@printf "  tools-install            Install pinned Go tools\n"
	@printf "\nContracts & codegen\n"
	@printf "  mock-gen                 Generate mocks (all services)\n"
	@printf "  mock-gen-*               Generate mocks in one service\n"
	@printf "  proto-check              Check protobuf compile\n"
	@printf "  proto-gen                Generate protobuf/gRPC (all)\n"
	@printf "  proto-gen-*              Generate protobuf/gRPC (single package)\n"
	@printf "  sqlc-gen                 Generate sqlc code\n"
	@printf "  sqlc-gen-*               Generate sqlc code in one service\n"
	@printf "  openapi-lint             Lint OpenAPI\n"
	@printf "  asyncapi-lint            Lint AsyncAPI\n"
	@printf "  openapi-gen-dto          Generate all service DTOs\n"
	@printf "  openapi-gen-*-dto        Generate DTOs for one service\n"
	@printf "  contracts                Validate proto + OpenAPI + AsyncAPI\n"
	@printf "\nRun & compose\n"
	@printf "  run-*                    Run single service via service Makefile\n"
	@printf "  compose-config           Validate compose config\n"
	@printf "  compose-up               Start full stack\n"
	@printf "  compose-up-data          Start DB/cache only\n"
	@printf "  compose-up-shared        Start Kafka + Schema Registry\n"
	@printf "  compose-up-observability Start observability stack\n"
	@printf "  compose-down             Stop stack\n"
	@printf "  compose-logs             Stream full stack logs\n"
	@printf "  compose-ps               Show stack status\n"
	@printf "\nMigrations\n"
	@printf "  migrate-create-*         Create migration in service\n"
	@printf "  migrate-up-*             Apply migrations in service\n"
	@printf "  migrate-up-checkout-local Apply all checkout migrations\n"
	@printf "\nTests\n"
	@printf "  unit-tests               Run all unit tests\n"
	@printf "  unit-tests-*             Run unit tests for one service\n"
	@printf "  integration-tests        Run all integration tests\n"
	@printf "  integration-tests-*      Run integration tests for one service\n"
	@printf "  e2e-tests                Run e2e tests\n"

# tools

tools-check:
	@command -v go >/dev/null || { printf "Missing tool: go\n"; exit 1; }
	@command -v protoc >/dev/null || { printf "Missing tool: protoc\n"; exit 1; }
	@command -v docker >/dev/null || { printf "Missing tool: docker\n"; exit 1; }
	@docker compose version >/dev/null || { printf "Missing docker compose plugin\n"; exit 1; }
	@{ command -v redocly >/dev/null || command -v npx >/dev/null; } || { printf "Missing tool: redocly or npx\n"; exit 1; }
	@printf "Tools check passed.\n"

tools-install:
	@GOWORK=off go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0
	@GOWORK=off go install github.com/pressly/goose/v3/cmd/goose@v3.27.0
	@GOWORK=off go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
	@GOWORK=off go install github.com/vektra/mockery/v3@v3.7.0
	@GOWORK=off go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	@GOWORK=off go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1

# generate contracts/mocks
mock-gen: $(addprefix mock-gen-,$(MOCK_GEN_SERVICES))

mock-gen-%:
	@$(MAKE) -C internal/$*-svc mock-gen

define PROTOC_GEN
	@protoc -I $(PROTO_SRC) --go_out=. --go_opt=module=$(PROTO_GO_MODULE) \
	--go-grpc_out=. --go-grpc_opt=module=$(PROTO_GO_MODULE) \
	$(1)
endef

define PROTOC_CHECK
	@tmp_file=$$(mktemp); \
	protoc -I $(PROTO_SRC) --descriptor_set_out=$$tmp_file $(1); \
	rm -f $$tmp_file
endef

proto-check:
	$(call PROTOC_CHECK,$(PROTO_FILES))

proto-gen-common:
	$(call PROTOC_GEN,$(PROTO_SRC)/common/v1/*.proto)

proto-gen-identity: proto-gen-common
	$(call PROTOC_GEN,$(PROTO_SRC)/identity/v1/*.proto)

proto-gen-catalog: proto-gen-common
	$(call PROTOC_GEN,$(PROTO_SRC)/catalog/v1/*.proto)

proto-gen-cart: proto-gen-common
	$(call PROTOC_GEN,$(PROTO_SRC)/cart/v1/*.proto)

proto-gen-order: proto-gen-common
	$(call PROTOC_GEN,$(PROTO_SRC)/order/v1/*.proto)

proto-gen-payment: proto-gen-common
	$(call PROTOC_GEN,$(PROTO_SRC)/payment/v1/*.proto)

proto-gen-notification: proto-gen-common
	$(call PROTOC_GEN,$(PROTO_SRC)/notification/v1/*.proto)

proto-gen: proto-gen-identity proto-gen-catalog proto-gen-cart proto-gen-order proto-gen-payment proto-gen-notification

sqlc-gen: $(addprefix sqlc-gen-,$(SQLC_GEN_SERVICES))

sqlc-gen-%:
	@$(MAKE) -C internal/$*-svc sqlc-gen

openapi-lint:
	@$(REDOCLY) lint --extends=minimal "$(OPENAPI_FILE)"

asyncapi-lint:
	@$(REDOCLY) lint "$(ASYNCAPI_FILE)"

openapi-gen-%-dto:
	@$(MAKE) -C internal/$*-svc openapi-gen-dto

openapi-gen-dto: $(OPENAPI_DTO_TARGETS)

contracts: proto-check openapi-lint asyncapi-lint

# compose
compose-config:
	@$(COMPOSE) $(COMPOSE_FILES) config >/dev/null
	@printf "Compose configuration is valid.\n"

compose-up:
	@$(COMPOSE) $(COMPOSE_FILES) up -d $(COMPOSE_SERVICES)

compose-up-data:
	@$(COMPOSE) $(COMPOSE_FILES) up -d $(COMPOSE_DATA_SERVICES)

compose-up-shared:
	@$(COMPOSE) $(COMPOSE_FILES) up -d $(COMPOSE_INFRA_SERVICES)

compose-up-observability:
	@$(COMPOSE) $(COMPOSE_FILES) up -d $(COMPOSE_OBSERVABILITY_SERVICES)

compose-down:
	@$(COMPOSE) $(COMPOSE_FILES) down

compose-logs:
	@$(COMPOSE) $(COMPOSE_FILES) logs -f $(COMPOSE_SERVICES)

compose-logs-data:
	@$(COMPOSE) $(COMPOSE_FILES) logs -f $(COMPOSE_DATA_SERVICES)

compose-logs-shared:
	@$(COMPOSE) $(COMPOSE_FILES) logs -f $(COMPOSE_INFRA_SERVICES)

compose-logs-observability:
	@$(COMPOSE) $(COMPOSE_FILES) logs -f $(COMPOSE_OBSERVABILITY_SERVICES)

compose-ps:
	@$(COMPOSE) $(COMPOSE_FILES) ps

# migrations
migrate-create-%:
	@$(MAKE) -C internal/$*-svc migrate-create name="$(name)"

migrate-up-%:
	@$(MAKE) -C internal/$*-svc migrate-up

migrate-up-checkout-local: $(addprefix migrate-up-,$(MIGRATION_SERVICES))

# running
run-%:
	@$(MAKE) -C internal/$*-svc run

# tests
unit-tests: unit-tests-common $(addprefix unit-tests-,$(SERVICES))

unit-tests-common:
	@cd internal/common && go test -v ./...

unit-tests-%:
	@$(MAKE) -C internal/$*-svc unit-tests

integration-tests: $(addprefix integration-tests-,$(SERVICES))

integration-tests-%:
	@$(MAKE) -C internal/$*-svc integration-tests

# e2e tests

e2e-checkout-up: compose-up

e2e-checkout-down: compose-down

e2e-tests:
	@cd tests/e2e && GOWORK=off go test -v ./... -count=1

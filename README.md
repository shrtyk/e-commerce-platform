# E-Commerce Platform

[![CI](https://github.com/shrtyk/e-commerce-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/shrtyk/e-commerce-platform/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/shrtyk/e-commerce-platform/graph/badge.svg?token=N6RLBNTER7)](https://codecov.io/gh/shrtyk/e-commerce-platform)

The platform is organized as a monorepo of focused services, with Kafka as the central integration bus.
Public traffic enters through the gateway over REST, while internal services communicate through gRPC and asynchronous Kafka events using Protobuf contracts.

## Why This Project Exists

This project was built to practice developing a production-style backend in Go.

Instead of creating another simple CRUD demo, the goal was to design an end-to-end e-commerce platform and to deal with problems common in real systems:

- service boundaries and domain ownership
- synchronous vs asynchronous communication
- eventual consistency across services
- event delivery reliability
- idempotent consumers and failure recovery
- observability across distributed services
- contract-first API development with OpenAPI and Protobuf

The focus is not on e-commerce itself, but on building and evolving a distributed system with a sprinkle of tradeoffs here and there.

## Architecture

- External ingress: `gateway` via `nginx` and public `OpenAPI`
- Internal sync communication: `gRPC + Protobuf`
- Internal async communication: `Kafka + Protobuf`
- Persistence: service-owned `PostgreSQL`, selective `Redis`
- Observability: `OpenTelemetry`, `Prometheus`, `Grafana`, `Loki`, `Tempo`
- Local runtime: `Docker Compose`

## Runtime Components

The platform currently includes the following core services:

- `identity` for registration, login, refresh flow, JWT issuing, and profile operations
- `catalog` implemented in the repository as `product-svc` for products, categories, pricing references, and stock ownership (better to separate stock as a stand-alone service later)
- `cart` for customer cart lifecycle and checkout preparation
- `order` as the checkout orchestrator and saga owner
- `payment` with a provider abstraction and stub provider
- `notification` consuming business events and sending stubbed notifications
- `gateway` as the public entrypoint

Local development also includes shared infrastructure such as Kafka, Schema Registry, PostgreSQL, Redis, and the observability stack.

## Core Principles

- Each service owns its own persistence boundary
- No cross-service database access
- Kafka is the central integration bus
- Event publication uses the transactional outbox pattern
- Consumers are expected to be idempotent
- Public contracts are `OpenAPI`; internal contracts are `Protobuf`
- `AsyncAPI` describes the event topology and follows protobuf changes

## Repository Layout

```text
.
├── api/                    # OpenAPI, AsyncAPI, and Protobuf contracts
├── configs/                # Shared runtime configuration assets
├── deploy/compose/         # Local Docker Compose manifests
├── internal/
│   ├── common/             # Shared Go packages used by services
│   ├── identity-svc/
│   ├── product-svc/
│   ├── cart-svc/
│   ├── order-svc/
│   ├── payment-svc/
│   └── notification-svc/
├── tests/e2e/              # End-to-end test module
├── Makefile                # Main developer entrypoint
├── go.work                 # Workspace wiring for modules
└── .github/workflows/      # CI workflows
```

## Per-Service Structure

Each service follows the same general shape:

```text
internal/<service>-svc/
├── cmd/app/                # Composition root and service boot
├── internal/
│   ├── app/                # Application lifecycle
│   ├── config/             # Service configuration loading and validation
│   ├── core/
│   │   ├── domain/         # Domain entities and value objects
│   │   ├── ports/          # Outbound interfaces
│   │   └── service/        # Business use cases
│   ├── adapters/
│   │   ├── inbound/        # HTTP and/or gRPC adapters
│   │   └── outbound/       # Postgres, Kafka, JWT, Redis, providers
│   ├── integration/        # Integration tests
│   └── testhelper/         # Test support code
├── build/                  # Service Dockerfiles and image assets
├── tools/                  # Service-local generation tool config
├── .env.example            # Non-secret local defaults
├── Makefile                # Service-local dev commands
└── go.mod
```

The exact adapter set differs by service, but the layering is consistent: domain and services stay isolated from transport and infrastructure concerns.

## Contracts

- `api/openapi/public/openapi.yaml`: public HTTP surface exposed through the gateway
- `api/proto/...`: canonical internal gRPC and event payload contracts
- `api/asyncapi/kafka-events.yaml`: descriptive event topology for Kafka channels

When contracts change, protobuf is the source of truth for internal APIs and event payloads.

## Local Development

### Prerequisites

Required local tools:

- `go`
- `protoc`
- `docker` with the Compose plugin
- `redocly` or `npx`
- `golangci-lint` for lint targets

Check required tooling:

```bash
make tools-check
```

Install pinned Go-side code generation tools:

```bash
make tools-install
```

### Start The Stack

Start the full local platform:

```bash
make compose-up
```

Stop and remove local containers and volumes:

```bash
make compose-down
```

Stream logs:

```bash
make compose-logs
```

Useful partial startup targets:

- `make compose-up-data`
- `make compose-up-shared`
- `make compose-up-observability`

### Generate And Validate Contracts

```bash
make proto-check
make proto-gen
make openapi-gen-dto
make contracts
```

You can also target a single service or package with service-specific make targets such as `make proto-gen-order` or `make openapi-gen-identity-dto`.

## Testing And Quality

Run all unit tests:

```bash
make unit-tests
```

Run unit tests for one service:

```bash
make unit-tests-identity
```

Run all integration tests:

```bash
make integration-tests
```

Run end-to-end tests:

```bash
make e2e-tests
```

Run linting across services:

```bash
make lint
```

## Current Status

- identity, catalog, cart, order, payment, and notification slices are implemented
- end-to-end checkout flow is covered, including compensation on payment failure
- messaging reliability and observability baselines are in place
- gateway and public API consolidation are complete

Current work is focused on the code quility, remaining security and delivery baseline, including the broader CI quality and security lane.

## First Places To Read In The Repository

For a quick orientation, start with:

- `Makefile`
- `api/openapi/public/openapi.yaml`
- `api/asyncapi/kafka-events.yaml`
- `api/proto/`
- one service end-to-end, for example `internal/identity-svc/` or `internal/order-svc/`
- `tests/e2e/`

## License

See `LICENSE`.

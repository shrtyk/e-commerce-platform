FROM golang:1.26.0-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

RUN apk add --no-cache git ca-certificates

RUN GOWORK=off CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go install github.com/pressly/goose/v3/cmd/goose@v3.27.0

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app -h /app app

WORKDIR /app

COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY internal/cart-svc/internal/adapters/outbound/postgres/migrations /app/migrations

RUN chown -R app:app /usr/local/bin/goose /app/migrations \
    && chmod 500 /usr/local/bin/goose \
    && chmod -R u=rwX,go= /app/migrations

USER app

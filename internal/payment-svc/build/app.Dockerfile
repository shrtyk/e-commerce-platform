FROM golang:1.26.0-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.work go.work.sum ./
COPY go.mod go.sum ./
COPY internal/common ./internal/common
COPY internal/cart-svc ./internal/cart-svc
COPY internal/identity-svc ./internal/identity-svc
COPY internal/notification-svc ./internal/notification-svc
COPY internal/order-svc ./internal/order-svc
COPY internal/payment-svc ./internal/payment-svc
COPY internal/product-svc ./internal/product-svc
COPY tests/e2e ./tests/e2e

WORKDIR /src/internal/payment-svc
RUN GOWORK=/src/go.work CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/service ./cmd/app

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app -h /app app

WORKDIR /app

COPY --from=builder /out/service /app/service

RUN chown app:app /app/service \
    && chmod 500 /app/service

USER app

EXPOSE 18085 19095

ENTRYPOINT ["/app/service"]

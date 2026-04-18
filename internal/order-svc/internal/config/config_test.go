package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	config "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/config"
)

func TestMustLoad(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REDIS_ADDR", "order-redis:6379")
	t.Setenv("SERVICE_HTTP_ADDR", ":18080")
	t.Setenv("SERVICE_GRPC_ADDR", ":19090")

	cfg := config.MustLoad()

	require.Equal(t, "order", cfg.Service.Name)
	require.Equal(t, ":18080", cfg.Service.HTTPAddr)
	require.Equal(t, ":19090", cfg.Service.GRPCAddr)
	require.True(t, cfg.Redis.Enabled)
	require.Equal(t, "order-redis:6379", cfg.Redis.Addr)
	require.Equal(t, 100, cfg.Relay.BatchSize)
	require.Equal(t, 500*time.Millisecond, cfg.Relay.Interval)
	require.Equal(t, time.Second, cfg.Relay.RetryBaseBackoff)
	require.Equal(t, 30*time.Second, cfg.Relay.RetryMaxBackoff)
	require.Equal(t, "order-svc-relay-1", cfg.Relay.WorkerID)
	require.Equal(t, 30*time.Second, cfg.Relay.StaleLockTTL)
}

func TestMustLoadDefaults(t *testing.T) {
	setRequiredEnv(t)
	require.NoError(t, os.Unsetenv("SERVICE_HTTP_ADDR"))
	require.NoError(t, os.Unsetenv("SERVICE_GRPC_ADDR"))
	require.NoError(t, os.Unsetenv("REDIS_ADDR"))

	cfg := config.MustLoad()

	require.Equal(t, ":8080", cfg.Service.HTTPAddr)
	require.Equal(t, ":9090", cfg.Service.GRPCAddr)
	require.False(t, cfg.Redis.Enabled)
	require.Equal(t, 100, cfg.Relay.BatchSize)
}

func TestMustLoadPanicsWhenRelayInvalid(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OUTBOX_RELAY_BATCH_SIZE", "0")

	require.Panics(t, func() {
		_ = config.MustLoad()
	})
}

func TestMustLoadPanicsWhenWorkerIDWhitespaceOnly(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OUTBOX_RELAY_WORKER_ID", "   ")

	require.Panics(t, func() {
		_ = config.MustLoad()
	})
}

func TestMustLoadPanicsWhenRequiredEnvMissing(t *testing.T) {
	setRequiredEnv(t)
	require.NoError(t, os.Unsetenv("POSTGRES_HOST"))

	require.Panics(t, func() {
		_ = config.MustLoad()
	})
}

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("SERVICE_NAME", "order")
	t.Setenv("POSTGRES_HOST", "order-postgres")
	t.Setenv("POSTGRES_DB", "order")
	t.Setenv("POSTGRES_USER", "order")
	t.Setenv("POSTGRES_PASSWORD", "order")
	t.Setenv("KAFKA_BROKERS", "kafka:9092")
	t.Setenv("SCHEMA_REGISTRY_URL", "http://schema-registry:8081")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")
}

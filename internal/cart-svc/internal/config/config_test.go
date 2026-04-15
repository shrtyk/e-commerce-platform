package config_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	config "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/config"
)

func TestMustLoad(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REDIS_ADDR", "cart-redis:6379")

	cfg := config.MustLoad()
	require.Equal(t, "cart", cfg.Service.Name)
	require.True(t, cfg.Redis.Enabled)
	require.Equal(t, "cart-redis:6379", cfg.Redis.Addr)
}

func TestMustLoadPanicsWhenRequiredEnvMissing(t *testing.T) {
	setRequiredEnv(t)
	require.NoError(t, os.Unsetenv("POSTGRES_HOST"))

	require.Panics(t, func() {
		_ = config.MustLoad()
	})
}

func TestMustLoadRedisDisabledWhenAddrEmpty(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REDIS_ADDR", "")

	cfg := config.MustLoad()
	require.False(t, cfg.Redis.Enabled)
	require.Equal(t, "", cfg.Redis.Addr)
}

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("SERVICE_NAME", "cart")
	t.Setenv("POSTGRES_HOST", "cart-postgres")
	t.Setenv("POSTGRES_DB", "cart")
	t.Setenv("POSTGRES_USER", "cart")
	t.Setenv("POSTGRES_PASSWORD", "cart")
	t.Setenv("KAFKA_BROKERS", "kafka:9092")
	t.Setenv("SCHEMA_REGISTRY_URL", "http://schema-registry:8081")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")
}

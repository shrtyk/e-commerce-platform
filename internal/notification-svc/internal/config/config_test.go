package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/config"
)

func TestMustLoad(t *testing.T) {
	t.Run("enables redis when redis addr is set", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("REDIS_ADDR", "redis:6379")

		cfg := config.MustLoad()

		require.True(t, cfg.Redis.Enabled)
	})

	t.Run("disables redis when redis addr is empty", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("REDIS_ADDR", "")

		cfg := config.MustLoad()

		require.False(t, cfg.Redis.Enabled)
	})
}

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("SERVICE_NAME", "notification-svc")
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_DB", "notification")
	t.Setenv("POSTGRES_USER", "notification")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("KAFKA_BROKERS", "kafka:9092")
	t.Setenv("SCHEMA_REGISTRY_URL", "http://schema-registry:8081")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")
}

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

	t.Run("loads order events defaults", func(t *testing.T) {
		setRequiredEnv(t)

		cfg := config.MustLoad()

		require.True(t, cfg.OrderEvents.Enabled)
		require.Equal(t, "order.events", cfg.OrderEvents.Topic)
		require.Equal(t, "notification-svc-order-events-v1", cfg.OrderEvents.GroupID)
		require.Equal(t, "500ms", cfg.OrderEvents.PollInterval.String())
	})

	t.Run("panics on blank order events topic", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "true")
		t.Setenv("ORDER_EVENTS_TOPIC", "  ")

		require.PanicsWithError(t, "field \"OrderEvents.Topic\" must be non-empty", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on blank order events group id", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "true")
		t.Setenv("ORDER_EVENTS_GROUP_ID", "   ")

		require.PanicsWithError(t, "field \"OrderEvents.GroupID\" must be non-empty", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on non-positive order events poll interval", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "true")
		t.Setenv("ORDER_EVENTS_POLL_INTERVAL", "0s")

		require.PanicsWithError(t, "field \"OrderEvents.PollInterval\" must be positive", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("does not validate order events fields when disabled", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "false")
		t.Setenv("ORDER_EVENTS_TOPIC", "   ")
		t.Setenv("ORDER_EVENTS_GROUP_ID", "   ")
		t.Setenv("ORDER_EVENTS_POLL_INTERVAL", "0s")

		require.NotPanics(t, func() {
			cfg := config.MustLoad()
			require.False(t, cfg.OrderEvents.Enabled)
		})
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

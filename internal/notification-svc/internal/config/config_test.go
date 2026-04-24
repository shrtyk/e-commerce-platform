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
		require.Equal(t, 3, cfg.OrderEvents.MaxRetryAttempts)
		require.Equal(t, 100, cfg.Relay.BatchSize)
		require.Equal(t, "500ms", cfg.Relay.Interval.String())
		require.Equal(t, "1s", cfg.Relay.RetryBaseBackoff.String())
		require.Equal(t, "30s", cfg.Relay.RetryMaxBackoff.String())
		require.Equal(t, "notification-svc-relay-1", cfg.Relay.WorkerID)
		require.Equal(t, "30s", cfg.Relay.StaleLockTTL.String())
	})

	t.Run("panics on non-positive relay batch size", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("OUTBOX_RELAY_BATCH_SIZE", "0")

		require.PanicsWithError(t, "field \"Relay.BatchSize\" must be positive", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on non-positive relay interval", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("OUTBOX_RELAY_INTERVAL", "0s")

		require.PanicsWithError(t, "field \"Relay.Interval\" must be positive", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on non-positive relay retry base backoff", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("OUTBOX_RELAY_RETRY_BASE_BACKOFF", "0s")

		require.PanicsWithError(t, "field \"Relay.RetryBaseBackoff\" must be positive", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on non-positive relay retry max backoff", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("OUTBOX_RELAY_RETRY_MAX_BACKOFF", "0s")

		require.PanicsWithError(t, "field \"Relay.RetryMaxBackoff\" must be positive", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics when relay retry base exceeds max", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("OUTBOX_RELAY_RETRY_BASE_BACKOFF", "10s")
		t.Setenv("OUTBOX_RELAY_RETRY_MAX_BACKOFF", "5s")

		require.PanicsWithError(t, "field \"Relay.RetryBaseBackoff\" must be less than or equal to Relay.RetryMaxBackoff", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on blank relay worker id", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("OUTBOX_RELAY_WORKER_ID", "  ")

		require.PanicsWithError(t, "field \"Relay.WorkerID\" must be non-empty", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on non-positive relay stale lock ttl", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("OUTBOX_RELAY_STALE_LOCK_TTL", "0s")

		require.PanicsWithError(t, "field \"Relay.StaleLockTTL\" must be positive", func() {
			_ = config.MustLoad()
		})
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

	t.Run("panics on non-positive order events max retry attempts", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "true")
		t.Setenv("ORDER_EVENTS_MAX_RETRY_ATTEMPTS", "0")

		require.PanicsWithError(t, "field \"OrderEvents.MaxRetryAttempts\" must be >= 1", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on blank policy default channel", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "true")
		t.Setenv("POLICY_DEFAULT_CHANNEL", "   ")

		require.PanicsWithError(t, "field \"Policy.DefaultChannel\" must be non-empty", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on blank policy confirmed template", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "true")
		t.Setenv("POLICY_ORDER_CONFIRMED_TEMPLATE", "")

		require.PanicsWithError(t, "field \"Policy.OrderConfirmedTemplate\" must be non-empty", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("panics on blank policy cancelled template", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "true")
		t.Setenv("POLICY_ORDER_CANCELLED_TEMPLATE", "\t")

		require.PanicsWithError(t, "field \"Policy.OrderCancelledTemplate\" must be non-empty", func() {
			_ = config.MustLoad()
		})
	})

	t.Run("does not validate order events fields when disabled", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("ORDER_EVENTS_ENABLED", "false")
		t.Setenv("ORDER_EVENTS_TOPIC", "   ")
		t.Setenv("ORDER_EVENTS_GROUP_ID", "   ")
		t.Setenv("ORDER_EVENTS_POLL_INTERVAL", "0s")
		t.Setenv("ORDER_EVENTS_MAX_RETRY_ATTEMPTS", "0")

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

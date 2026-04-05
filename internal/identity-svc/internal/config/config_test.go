package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	config "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/config"
)

func TestMustLoadAuthConfig(t *testing.T) {
	t.Setenv("SERVICE_NAME", "identity")
	t.Setenv("POSTGRES_HOST", "identity-postgres")
	t.Setenv("POSTGRES_DB", "identity")
	t.Setenv("POSTGRES_USER", "identity")
	t.Setenv("POSTGRES_PASSWORD", "identity")
	t.Setenv("KAFKA_BROKERS", "kafka:9092")
	t.Setenv("SCHEMA_REGISTRY_URL", "http://schema-registry:8081")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")
	t.Setenv("AUTH_SESSION_TTL", "24h")
	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "15m")
	t.Setenv("AUTH_ACCESS_TOKEN_KEY", "secret-key")
	t.Setenv("AUTH_ACCESS_TOKEN_ISSUER", "identity-svc")

	cfg := config.MustLoad()
	require.Equal(t, 24*time.Hour, cfg.Auth.SessionTTL)
	require.Equal(t, 15*time.Minute, cfg.Auth.AccessTokenTTL)
	require.Equal(t, "secret-key", cfg.Auth.AccessTokenKey)
	require.Equal(t, "identity-svc", cfg.Auth.AccessTokenIssuer)
}

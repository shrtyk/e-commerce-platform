package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	config "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/config"
)

func TestMustLoad(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REDIS_ADDR", "cart-redis:6379")
	t.Setenv("CATALOG_GRPC_ADDR", "catalog:9090")
	t.Setenv("AUTH_ACCESS_TOKEN_ISSUER", "identity-svc")

	cfg := config.MustLoad()
	require.Equal(t, "cart", cfg.Service.Name)
	require.True(t, cfg.Redis.Enabled)
	require.Equal(t, "cart-redis:6379", cfg.Redis.Addr)
	require.Equal(t, "catalog:9090", cfg.Catalog.GRPCAddr)
	require.Equal(t, "test-secret", cfg.Auth.AccessTokenKey)
	require.Equal(t, "identity-svc", cfg.Auth.AccessTokenIssuer)
	require.Equal(t, 5*time.Minute, cfg.Cache.ActiveCartTTL)
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

func TestMustLoadCatalogGRPCDefault(t *testing.T) {
	setRequiredEnv(t)
	require.NoError(t, os.Unsetenv("CATALOG_GRPC_ADDR"))

	cfg := config.MustLoad()
	require.Equal(t, "product-svc:9090", cfg.Catalog.GRPCAddr)
}

func TestMustLoadActiveCartTTLFromEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CART_CACHE_ACTIVE_CART_TTL", "45s")

	cfg := config.MustLoad()
	require.Equal(t, 45*time.Second, cfg.Cache.ActiveCartTTL)
}

func TestMustLoadAuthIssuerDefault(t *testing.T) {
	setRequiredEnv(t)
	require.NoError(t, os.Unsetenv("AUTH_ACCESS_TOKEN_ISSUER"))

	cfg := config.MustLoad()
	require.Equal(t, "ecom-identity-svc", cfg.Auth.AccessTokenIssuer)
}

func TestMustLoadPanicsWhenAuthKeyMissing(t *testing.T) {
	setRequiredEnv(t)
	require.NoError(t, os.Unsetenv("AUTH_ACCESS_TOKEN_KEY"))

	require.Panics(t, func() {
		_ = config.MustLoad()
	})
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
	t.Setenv("AUTH_ACCESS_TOKEN_KEY", "test-secret")
}

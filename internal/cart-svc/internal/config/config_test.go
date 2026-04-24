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

func TestMustLoadPanicsWhenAuthKeyInvalid(t *testing.T) {
	testCases := []struct {
		name            string
		prepareEnv      func(t *testing.T)
		expectedMessage string
	}{
		{
			name: "missing",
			prepareEnv: func(t *testing.T) {
				t.Helper()
				require.NoError(t, os.Unsetenv("AUTH_ACCESS_TOKEN_KEY"))
			},
			expectedMessage: "field \"AccessTokenKey\" is required but the value is not provided",
		},
		{
			name: "whitespace only",
			prepareEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("AUTH_ACCESS_TOKEN_KEY", "   ")
			},
			expectedMessage: "field \"Auth.AccessTokenKey\" must be non-empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			tc.prepareEnv(t)

			if tc.name == "missing" {
				requireMustLoadPanicContains(t, tc.expectedMessage)
				return
			}

			require.PanicsWithError(t, tc.expectedMessage, func() {
				_ = config.MustLoad()
			})
		})
	}
}

func TestMustLoadPanicsWhenAuthIssuerInvalid(t *testing.T) {
	testCases := []struct {
		name            string
		prepareEnv      func(t *testing.T)
		expectedMessage string
	}{
		{
			name: "missing",
			prepareEnv: func(t *testing.T) {
				t.Helper()
				require.NoError(t, os.Unsetenv("AUTH_ACCESS_TOKEN_ISSUER"))
			},
			expectedMessage: "field \"AccessTokenIssuer\" is required but the value is not provided",
		},
		{
			name: "whitespace only",
			prepareEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("AUTH_ACCESS_TOKEN_ISSUER", "   ")
			},
			expectedMessage: "field \"Auth.AccessTokenIssuer\" must be non-empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			tc.prepareEnv(t)

			if tc.name == "missing" {
				requireMustLoadPanicContains(t, tc.expectedMessage)
				return
			}

			require.PanicsWithError(t, tc.expectedMessage, func() {
				_ = config.MustLoad()
			})
		})
	}
}

func TestMustLoadPanicsWhenCatalogGRPCAddrInvalid(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace only", value: "   "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("CATALOG_GRPC_ADDR", tc.value)

			require.PanicsWithError(t, "field \"Catalog.GRPCAddr\" must be non-empty", func() {
				_ = config.MustLoad()
			})
		})
	}
}

func TestMustLoadPanicsWhenActiveCartTTLNonPositive(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "zero", value: "0s"},
		{name: "negative", value: "-1s"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("CART_CACHE_ACTIVE_CART_TTL", tc.value)

			require.PanicsWithError(t, "field \"Cache.ActiveCartTTL\" must be positive", func() {
				_ = config.MustLoad()
			})
		})
	}
}

func requireMustLoadPanicContains(t *testing.T, expectedMessage string) {
	t.Helper()

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered)

		if recoveredError, ok := recovered.(error); ok {
			require.Contains(t, recoveredError.Error(), expectedMessage)
			return
		}

		recoveredString, ok := recovered.(string)
		require.True(t, ok, "panic value should be error or string")
		require.Contains(t, recoveredString, expectedMessage)
	}()

	_ = config.MustLoad()
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
	t.Setenv("AUTH_ACCESS_TOKEN_ISSUER", "ecom-identity-svc")
}

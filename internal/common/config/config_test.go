package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	config "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

func TestMustLoad(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		panicMsg string
		assertFn func(t *testing.T, cfg config.Config)
	}{
		{
			name: "defaults",
			env: map[string]string{
				"SERVICE_NAME":                "identity",
				"POSTGRES_HOST":               "identity-postgres",
				"POSTGRES_DB":                 "identity",
				"POSTGRES_USER":               "identity",
				"POSTGRES_PASSWORD":           "identity",
				"KAFKA_BROKERS":               "kafka:9092",
				"SCHEMA_REGISTRY_URL":         "http://schema-registry:8081",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "otel-collector:4317",
			},
			assertFn: func(t *testing.T, cfg config.Config) {
				is := assert.New(t)

				is.Equal("identity", cfg.Service.Name)
				is.Equal("local", cfg.Service.Environment)
				is.Equal(":8080", cfg.Service.HTTPAddr)
				is.Equal(":9090", cfg.Service.GRPCAddr)
				is.Equal("5432", cfg.Postgres.Port)
				is.Equal("disable", cfg.Postgres.SSLMode)
				is.False(cfg.Redis.Enabled)
				is.False(cfg.OTel.Insecure)
				is.Equal(
					"postgres://identity:identity@identity-postgres:5432/identity?sslmode=disable",
					cfg.Postgres.DSN(),
				)
			},
		},
		{
			name: "missing schema registry",
			env: map[string]string{
				"SERVICE_NAME":                "identity",
				"POSTGRES_HOST":               "identity-postgres",
				"POSTGRES_DB":                 "identity",
				"POSTGRES_USER":               "identity",
				"POSTGRES_PASSWORD":           "identity",
				"KAFKA_BROKERS":               "kafka:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "otel-collector:4317",
			},
			panicMsg: `field "URL" is required but the value is not provided`,
		},
		{
			name: "optional redis",
			env: map[string]string{
				"SERVICE_NAME":                "cart",
				"POSTGRES_HOST":               "cart-postgres",
				"POSTGRES_DB":                 "cart",
				"POSTGRES_USER":               "cart",
				"POSTGRES_PASSWORD":           "cart",
				"KAFKA_BROKERS":               "kafka:9092",
				"SCHEMA_REGISTRY_URL":         "http://schema-registry:8081",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "otel-collector:4317",
				"OTEL_EXPORTER_OTLP_INSECURE": "true",
				"REDIS_ADDR":                  "cart-redis:6379",
			},
			assertFn: func(t *testing.T, cfg config.Config) {
				is := assert.New(t)

				is.True(cfg.Redis.Enabled)
				is.Equal("cart-redis:6379", cfg.Redis.Addr)
				is.True(cfg.OTel.Insecure)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			if tt.panicMsg != "" {
				require.PanicsWithError(t, tt.panicMsg, func() {
					config.MustLoad()
				})
				return
			}

			cfg := config.MustLoad()
			tt.assertFn(t, cfg)
		})
	}
}

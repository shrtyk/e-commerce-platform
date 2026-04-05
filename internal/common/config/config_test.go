package config_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	config "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

const (
	defaultKafkaBrokers = "kafka:9092"
	schemaRegistryURL   = "http://schema-registry:8081"
	otelEndpoint        = "otel-collector:4317"
)

func serviceEnv(service string) map[string]string {
	return map[string]string{
		"SERVICE_NAME":                service,
		"POSTGRES_HOST":               fmt.Sprintf("%s-postgres", service),
		"POSTGRES_DB":                 service,
		"POSTGRES_USER":               service,
		"POSTGRES_PASSWORD":           service,
		"KAFKA_BROKERS":               defaultKafkaBrokers,
		"SCHEMA_REGISTRY_URL":         schemaRegistryURL,
		"OTEL_EXPORTER_OTLP_ENDPOINT": otelEndpoint,
	}
}

func TestMustLoad(t *testing.T) {
	identityEnv := serviceEnv("identity")
	missingSchemaRegistryEnv := serviceEnv("identity")
	delete(missingSchemaRegistryEnv, "SCHEMA_REGISTRY_URL")
	cartEnv := serviceEnv("cart")
	cartEnv["OTEL_EXPORTER_OTLP_INSECURE"] = "true"
	cartEnv["REDIS_ADDR"] = "cart-redis:6379"

	tests := []struct {
		name     string
		env      map[string]string
		panicMsg string
		assertFn func(t *testing.T, cfg config.Config)
	}{
		{
			name: "defaults",
			env:  identityEnv,
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
			name:     "missing schema registry",
			env:      missingSchemaRegistryEnv,
			panicMsg: `field "URL" is required but the value is not provided`,
		},
		{
			name: "optional redis",
			env:  cartEnv,
			assertFn: func(t *testing.T, cfg config.Config) {
				is := assert.New(t)

				is.True(cfg.Redis.Enabled)
				is.Equal(cartEnv["REDIS_ADDR"], cfg.Redis.Addr)
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

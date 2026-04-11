package config_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/config"
)

const helperProcessEnv = "GO_WANT_PRODUCT_CONFIG_HELPER_PROCESS"

func TestMustLoad(t *testing.T) {
	t.Run("loads embedded common config from env", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("SERVICE_HTTP_ADDR", ":18080")

		cfg := config.MustLoad()

		require.Equal(t, "product-svc", cfg.Service.Name)
		require.Equal(t, ":18080", cfg.Service.HTTPAddr)
		require.Equal(t, "postgres", cfg.Postgres.Host)
		require.Equal(t, "product", cfg.Postgres.Database)
		require.Equal(t, "product", cfg.Postgres.User)
		require.Equal(t, "secret", cfg.Postgres.Password)
		require.False(t, cfg.Redis.Enabled)
		require.Equal(t, ":9090", cfg.Service.GRPCAddr)
		require.Equal(t, 10*time.Second, cfg.Timeouts.Shutdown)
	})

	t.Run("panics when required field is missing", func(t *testing.T) {
		tests := []struct {
			name      string
			key       string
			wantInErr string
		}{
			{name: "missing service name", key: "SERVICE_NAME", wantInErr: "field \"Name\" is required"},
			{name: "missing postgres host", key: "POSTGRES_HOST", wantInErr: "field \"Host\" is required"},
			{name: "missing postgres db", key: "POSTGRES_DB", wantInErr: "field \"Database\" is required"},
			{name: "missing postgres user", key: "POSTGRES_USER", wantInErr: "field \"User\" is required"},
			{name: "missing postgres password", key: "POSTGRES_PASSWORD", wantInErr: "field \"Password\" is required"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stderr, runErr := runHelperProcess(t, tt.key)

				require.Error(t, runErr)
				require.Contains(t, stderr, tt.wantInErr)
			})
		}
	})
}

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("SERVICE_NAME", "product-svc")
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_DB", "product")
	t.Setenv("POSTGRES_USER", "product")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("KAFKA_BROKERS", "kafka:9092")
	t.Setenv("SCHEMA_REGISTRY_URL", "http://schema-registry:8081")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")
}

func TestConfigMustLoadHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		t.Skip("helper process")
	}

	_ = config.MustLoad()
}

func runHelperProcess(t *testing.T, missingKey string) (string, error) {
	t.Helper()

	args := []string{"-test.run", "TestConfigMustLoadHelperProcess"}
	cmd := exec.Command(os.Args[0], args...)

	env := []string{
		helperProcessEnv + "=1",
		"SERVICE_NAME=product-svc",
		"POSTGRES_HOST=postgres",
		"POSTGRES_DB=product",
		"POSTGRES_USER=product",
		"POSTGRES_PASSWORD=secret",
		"KAFKA_BROKERS=kafka:9092",
		"SCHEMA_REGISTRY_URL=http://schema-registry:8081",
		"OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317",
	}

	filtered := make([]string, 0, len(env))
	for _, item := range env {
		if len(item) >= len(missingKey)+1 && item[:len(missingKey)+1] == missingKey+"=" {
			continue
		}

		filtered = append(filtered, item)
	}

	cmd.Env = filtered

	output, err := cmd.CombinedOutput()

	return string(output), err
}

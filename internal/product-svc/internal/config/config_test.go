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
		require.Equal(t, 100, cfg.Relay.BatchSize)
		require.Equal(t, 500*time.Millisecond, cfg.Relay.Interval)
		require.Equal(t, time.Second, cfg.Relay.RetryBaseBackoff)
		require.Equal(t, 30*time.Second, cfg.Relay.RetryMaxBackoff)
		require.Equal(t, "product-svc-relay-1", cfg.Relay.WorkerID)
		require.Equal(t, 30*time.Second, cfg.Relay.StaleLockTTL)
		require.Equal(t, "product-svc-test-key", cfg.Auth.AccessTokenKey)
		require.Equal(t, "ecom-identity-svc", cfg.Auth.AccessTokenIssuer)
		require.Equal(t, int32(100), cfg.Policy.ListPageSize)
		require.Equal(t, int64(1<<20), cfg.Policy.PatchMaxBodyBytes)
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

	t.Run("panics when relay config invalid", func(t *testing.T) {
		tests := []struct {
			name      string
			key       string
			value     string
			extra     []string
			wantInErr string
		}{
			{name: "relay batch size zero", key: "OUTBOX_RELAY_BATCH_SIZE", value: "0", wantInErr: "Relay.BatchSize"},
			{name: "relay interval zero", key: "OUTBOX_RELAY_INTERVAL", value: "0s", wantInErr: "Relay.Interval"},
			{name: "relay base backoff zero", key: "OUTBOX_RELAY_RETRY_BASE_BACKOFF", value: "0s", wantInErr: "Relay.RetryBaseBackoff"},
			{name: "relay max backoff zero", key: "OUTBOX_RELAY_RETRY_MAX_BACKOFF", value: "0s", wantInErr: "Relay.RetryMaxBackoff"},
			{name: "relay base above max", key: "OUTBOX_RELAY_RETRY_BASE_BACKOFF", value: "31s", extra: []string{"OUTBOX_RELAY_RETRY_MAX_BACKOFF=30s"}, wantInErr: "less than or equal"},
			{name: "relay worker id empty", key: "OUTBOX_RELAY_WORKER_ID", value: "   ", wantInErr: "Relay.WorkerID"},
			{name: "relay stale lock ttl zero", key: "OUTBOX_RELAY_STALE_LOCK_TTL", value: "0s", wantInErr: "Relay.StaleLockTTL"},
			{name: "policy list page size zero", key: "POLICY_LIST_PAGE_SIZE", value: "0", wantInErr: "Policy.ListPageSize"},
			{name: "policy patch max body bytes zero", key: "POLICY_PATCH_MAX_BODY_BYTES", value: "0", wantInErr: "Policy.PatchMaxBodyBytes"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stderr, runErr := runHelperProcessWithOverride(t, tt.key, tt.value, tt.extra...)

				require.Error(t, runErr)
				require.Contains(t, stderr, tt.wantInErr)
			})
		}
	})

	t.Run("panics when auth key invalid", func(t *testing.T) {
		tests := []struct {
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

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				setRequiredEnv(t)
				tt.prepareEnv(t)

				if tt.name == "missing" {
					requireMustLoadPanicContains(t, tt.expectedMessage)
					return
				}

				require.PanicsWithError(t, tt.expectedMessage, func() {
					_ = config.MustLoad()
				})
			})
		}
	})

	t.Run("panics when auth issuer invalid", func(t *testing.T) {
		tests := []struct {
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

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				setRequiredEnv(t)
				tt.prepareEnv(t)

				if tt.name == "missing" {
					requireMustLoadPanicContains(t, tt.expectedMessage)
					return
				}

				require.PanicsWithError(t, tt.expectedMessage, func() {
					_ = config.MustLoad()
				})
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
	t.Setenv("AUTH_ACCESS_TOKEN_KEY", "product-svc-test-key")
	t.Setenv("AUTH_ACCESS_TOKEN_ISSUER", "ecom-identity-svc")
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
			"AUTH_ACCESS_TOKEN_KEY=product-svc-test-key",
			"AUTH_ACCESS_TOKEN_ISSUER=ecom-identity-svc",
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

func runHelperProcessWithOverride(t *testing.T, key, value string, extra ...string) (string, error) {
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
		"AUTH_ACCESS_TOKEN_KEY=product-svc-test-key",
		"AUTH_ACCESS_TOKEN_ISSUER=ecom-identity-svc",
	}

	if key != "" {
		env = append(env, key+"="+value)
	}

	env = append(env, extra...)

	cmd.Env = env

	output, err := cmd.CombinedOutput()

	return string(output), err
}

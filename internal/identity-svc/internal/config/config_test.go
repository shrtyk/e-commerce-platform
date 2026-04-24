package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	config "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/config"
)

func setBaseRequiredEnv(t *testing.T) {
	t.Helper()

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
}

func TestMustLoadAuthConfig(t *testing.T) {
	setBaseRequiredEnv(t)
	t.Setenv("BOOTSTRAP_ADMIN_ENABLED", "true")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "bootstrap-admin@example.com")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("BOOTSTRAP_ADMIN_DISPLAY_NAME", "Bootstrap Admin")

	cfg := config.MustLoad()
	require.Equal(t, 24*time.Hour, cfg.Auth.SessionTTL)
	require.Equal(t, 15*time.Minute, cfg.Auth.AccessTokenTTL)
	require.Equal(t, "secret-key", cfg.Auth.AccessTokenKey)
	require.Equal(t, "identity-svc", cfg.Auth.AccessTokenIssuer)
	require.Equal(t, 8, cfg.Auth.PasswordMinLength)
	require.Equal(t, 10, cfg.Auth.BcryptCost)
	require.True(t, cfg.Bootstrap.Enabled)
	require.Equal(t, "bootstrap-admin@example.com", cfg.Bootstrap.Email)
	require.Equal(t, "bootstrap-secret", cfg.Bootstrap.Password)
	require.Equal(t, "Bootstrap Admin", cfg.Bootstrap.DisplayName)
}

func TestMustLoadPanicsWhenBootstrapEnabledWithoutEmail(t *testing.T) {
	setBaseRequiredEnv(t)
	t.Setenv("BOOTSTRAP_ADMIN_ENABLED", "true")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("BOOTSTRAP_ADMIN_DISPLAY_NAME", "Bootstrap Admin")

	require.PanicsWithError(t, "config: bootstrap admin: BOOTSTRAP_ADMIN_EMAIL is required when BOOTSTRAP_ADMIN_ENABLED=true", func() {
		_ = config.MustLoad()
	})
}

func TestMustLoadPanicsWhenBootstrapEnabledWithoutPassword(t *testing.T) {
	setBaseRequiredEnv(t)
	t.Setenv("BOOTSTRAP_ADMIN_ENABLED", "true")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "bootstrap-admin@example.com")
	t.Setenv("BOOTSTRAP_ADMIN_DISPLAY_NAME", "Bootstrap Admin")

	require.PanicsWithError(t, "config: bootstrap admin: BOOTSTRAP_ADMIN_PASSWORD is required when BOOTSTRAP_ADMIN_ENABLED=true", func() {
		_ = config.MustLoad()
	})
}

func TestMustLoadPanicsWhenPasswordPolicyInvalid(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		wantErr  string
	}{
		{name: "password min below one", envKey: "AUTH_PASSWORD_MIN_LENGTH", envValue: "0", wantErr: "field \"Auth.PasswordMinLength\" must be >= 1"},
		{name: "password min above max", envKey: "AUTH_PASSWORD_MIN_LENGTH", envValue: "73", wantErr: "field \"Auth.PasswordMinLength\" must be <= 72"},
		{name: "bcrypt cost below range", envKey: "AUTH_BCRYPT_COST", envValue: "3", wantErr: "field \"Auth.BcryptCost\" must be between 4 and 31"},
		{name: "bcrypt cost above range", envKey: "AUTH_BCRYPT_COST", envValue: "32", wantErr: "field \"Auth.BcryptCost\" must be between 4 and 31"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBaseRequiredEnv(t)
			t.Setenv(tt.envKey, tt.envValue)

			require.PanicsWithError(t, tt.wantErr, func() {
				_ = config.MustLoad()
			})
		})
	}
}

func TestMustLoadPanicsWhenAuthTokenEnvMissing(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
	}{
		{name: "access token key missing", envKey: "AUTH_ACCESS_TOKEN_KEY"},
		{name: "access token issuer missing", envKey: "AUTH_ACCESS_TOKEN_ISSUER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBaseRequiredEnv(t)
			t.Setenv(tt.envKey, "")

			require.Panics(t, func() {
				_ = config.MustLoad()
			})
		})
	}
}

func TestMustLoadPanicsWhenAuthTokenEnvWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		wantErr  string
		envValue string
	}{
		{
			name:     "access token key whitespace",
			envKey:   "AUTH_ACCESS_TOKEN_KEY",
			envValue: "   ",
			wantErr:  "field \"Auth.AccessTokenKey\" must be non-empty",
		},
		{
			name:     "access token issuer whitespace",
			envKey:   "AUTH_ACCESS_TOKEN_ISSUER",
			envValue: "   ",
			wantErr:  "field \"Auth.AccessTokenIssuer\" must be non-empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBaseRequiredEnv(t)
			t.Setenv(tt.envKey, tt.envValue)

			require.PanicsWithError(t, tt.wantErr, func() {
				_ = config.MustLoad()
			})
		})
	}
}

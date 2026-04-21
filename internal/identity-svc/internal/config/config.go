package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	commoncfg "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

type Config struct {
	commoncfg.Config
	Auth      Auth      `env-prefix:"AUTH_"`
	Bootstrap Bootstrap `env-prefix:"BOOTSTRAP_ADMIN_"`
}

type Auth struct {
	SessionTTL        time.Duration `env:"SESSION_TTL" env-default:"168h"`
	AccessTokenTTL    time.Duration `env:"ACCESS_TOKEN_TTL" env-default:"15m"`
	AccessTokenKey    string        `env:"ACCESS_TOKEN_KEY" env-required:"true"`
	AccessTokenIssuer string        `env:"ACCESS_TOKEN_ISSUER" env-default:"ecom-identity-svc"`
}

type Bootstrap struct {
	Enabled     bool   `env:"ENABLED" env-default:"false"`
	Email       string `env:"EMAIL"`
	Password    string `env:"PASSWORD"`
	DisplayName string `env:"DISPLAY_NAME"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	if err := validateBootstrap(cfg.Bootstrap); err != nil {
		panic(fmt.Errorf("config: %w", err))
	}

	return cfg
}

func validateBootstrap(cfg Bootstrap) error {
	if !cfg.Enabled {
		return nil
	}

	if strings.TrimSpace(cfg.Email) == "" {
		return fmt.Errorf("bootstrap admin: BOOTSTRAP_ADMIN_EMAIL is required when BOOTSTRAP_ADMIN_ENABLED=true")
	}

	if strings.TrimSpace(cfg.Password) == "" {
		return fmt.Errorf("bootstrap admin: BOOTSTRAP_ADMIN_PASSWORD is required when BOOTSTRAP_ADMIN_ENABLED=true")
	}

	return nil
}

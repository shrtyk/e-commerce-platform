package config

import (
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	commoncfg "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

type Config struct {
	commoncfg.Config
	Auth Auth `env-prefix:"AUTH_"`
}

type Auth struct {
	SessionTTL        time.Duration `env:"SESSION_TTL" env-default:"168h"`
	AccessTokenTTL    time.Duration `env:"ACCESS_TOKEN_TTL" env-default:"15m"`
	AccessTokenKey    string        `env:"ACCESS_TOKEN_KEY" env-required:"true"`
	AccessTokenIssuer string        `env:"ACCESS_TOKEN_ISSUER" env-default:"ecom-identity-svc"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	return cfg
}

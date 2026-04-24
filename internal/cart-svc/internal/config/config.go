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
	Catalog Catalog
	Auth    Auth  `env-prefix:"AUTH_"`
	Cache   Cache `env-prefix:"CART_CACHE_"`
}

type Catalog struct {
	GRPCAddr string `env:"CATALOG_GRPC_ADDR" env-default:"product-svc:9090"`
}

type Cache struct {
	ActiveCartTTL time.Duration `env:"ACTIVE_CART_TTL" env-default:"5m"`
}

type Auth struct {
	AccessTokenKey    string `env:"ACCESS_TOKEN_KEY" env-required:"true"`
	AccessTokenIssuer string `env:"ACCESS_TOKEN_ISSUER" env-required:"true"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	if strings.TrimSpace(cfg.Auth.AccessTokenKey) == "" {
		panic(fmt.Errorf("field \"Auth.AccessTokenKey\" must be non-empty"))
	}

	if strings.TrimSpace(cfg.Auth.AccessTokenIssuer) == "" {
		panic(fmt.Errorf("field \"Auth.AccessTokenIssuer\" must be non-empty"))
	}

	if strings.TrimSpace(cfg.Catalog.GRPCAddr) == "" {
		panic(fmt.Errorf("field \"Catalog.GRPCAddr\" must be non-empty"))
	}

	if cfg.Cache.ActiveCartTTL <= 0 {
		panic(fmt.Errorf("field \"Cache.ActiveCartTTL\" must be positive"))
	}

	return cfg
}

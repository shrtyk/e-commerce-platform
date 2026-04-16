package config

import (
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	commoncfg "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

type Config struct {
	commoncfg.Config
	Catalog Catalog
	Cache   Cache `env-prefix:"CART_CACHE_"`
}

type Catalog struct {
	GRPCAddr string `env:"CATALOG_GRPC_ADDR" env-default:"product-svc:9090"`
}

type Cache struct {
	ActiveCartTTL time.Duration `env:"ACTIVE_CART_TTL" env-default:"5m"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	return cfg
}

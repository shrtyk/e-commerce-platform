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
	Relay Relay `env-prefix:"OUTBOX_RELAY_"`
}

type Relay struct {
	BatchSize        int           `env:"BATCH_SIZE" env-default:"100"`
	Interval         time.Duration `env:"INTERVAL" env-default:"500ms"`
	RetryBaseBackoff time.Duration `env:"RETRY_BASE_BACKOFF" env-default:"1s"`
	RetryMaxBackoff  time.Duration `env:"RETRY_MAX_BACKOFF" env-default:"30s"`
	WorkerID         string        `env:"WORKER_ID" env-default:"product-svc-relay-1"`
	StaleLockTTL     time.Duration `env:"STALE_LOCK_TTL" env-default:"30s"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	if cfg.Relay.BatchSize < 1 {
		panic(fmt.Errorf("field \"Relay.BatchSize\" must be positive"))
	}

	if cfg.Relay.Interval <= 0 {
		panic(fmt.Errorf("field \"Relay.Interval\" must be positive"))
	}

	if cfg.Relay.RetryBaseBackoff <= 0 {
		panic(fmt.Errorf("field \"Relay.RetryBaseBackoff\" must be positive"))
	}

	if cfg.Relay.RetryMaxBackoff <= 0 {
		panic(fmt.Errorf("field \"Relay.RetryMaxBackoff\" must be positive"))
	}

	if cfg.Relay.RetryBaseBackoff > cfg.Relay.RetryMaxBackoff {
		panic(fmt.Errorf("field \"Relay.RetryBaseBackoff\" must be less than or equal to Relay.RetryMaxBackoff"))
	}

	if strings.TrimSpace(cfg.Relay.WorkerID) == "" {
		panic(fmt.Errorf("field \"Relay.WorkerID\" must be non-empty"))
	}

	if cfg.Relay.StaleLockTTL <= 0 {
		panic(fmt.Errorf("field \"Relay.StaleLockTTL\" must be positive"))
	}

	return cfg
}

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
	OrderEvents OrderEvents `env-prefix:"ORDER_EVENTS_"`
}

type OrderEvents struct {
	Enabled      bool          `env:"ENABLED" env-default:"true"`
	Topic        string        `env:"TOPIC" env-default:"order.events"`
	GroupID      string        `env:"GROUP_ID" env-default:"notification-svc-order-events-v1"`
	PollInterval time.Duration `env:"POLL_INTERVAL" env-default:"500ms"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	if !cfg.OrderEvents.Enabled {
		return cfg
	}

	if strings.TrimSpace(cfg.OrderEvents.Topic) == "" {
		panic(fmt.Errorf("field \"OrderEvents.Topic\" must be non-empty"))
	}

	if strings.TrimSpace(cfg.OrderEvents.GroupID) == "" {
		panic(fmt.Errorf("field \"OrderEvents.GroupID\" must be non-empty"))
	}

	if cfg.OrderEvents.PollInterval <= 0 {
		panic(fmt.Errorf("field \"OrderEvents.PollInterval\" must be positive"))
	}

	return cfg
}

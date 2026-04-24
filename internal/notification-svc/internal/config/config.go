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
	Policy      Policy      `env-prefix:"POLICY_"`
	Relay       Relay       `env-prefix:"OUTBOX_RELAY_"`
}

type OrderEvents struct {
	Enabled          bool          `env:"ENABLED" env-default:"true"`
	Topic            string        `env:"TOPIC" env-default:"order.events"`
	GroupID          string        `env:"GROUP_ID" env-default:"notification-svc-order-events-v1"`
	PollInterval     time.Duration `env:"POLL_INTERVAL" env-default:"500ms"`
	MaxRetryAttempts int           `env:"MAX_RETRY_ATTEMPTS" env-default:"3"`
}

type Policy struct {
	DefaultChannel         string `env:"DEFAULT_CHANNEL" env-default:"in_app"`
	OrderConfirmedTemplate string `env:"ORDER_CONFIRMED_TEMPLATE" env-default:"order %s confirmed"`
	OrderCancelledTemplate string `env:"ORDER_CANCELLED_TEMPLATE" env-default:"order %s cancelled: %s"`
}

type Relay struct {
	BatchSize        int           `env:"BATCH_SIZE" env-default:"100"`
	Interval         time.Duration `env:"INTERVAL" env-default:"500ms"`
	RetryBaseBackoff time.Duration `env:"RETRY_BASE_BACKOFF" env-default:"1s"`
	RetryMaxBackoff  time.Duration `env:"RETRY_MAX_BACKOFF" env-default:"30s"`
	WorkerID         string        `env:"WORKER_ID" env-default:"notification-svc-relay-1"`
	StaleLockTTL     time.Duration `env:"STALE_LOCK_TTL" env-default:"30s"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	if !cfg.OrderEvents.Enabled {
		validateRelay(cfg.Relay)
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

	if cfg.OrderEvents.MaxRetryAttempts < 1 {
		panic(fmt.Errorf("field \"OrderEvents.MaxRetryAttempts\" must be >= 1"))
	}

	if strings.TrimSpace(cfg.Policy.DefaultChannel) == "" {
		panic(fmt.Errorf("field \"Policy.DefaultChannel\" must be non-empty"))
	}

	if strings.TrimSpace(cfg.Policy.OrderConfirmedTemplate) == "" {
		panic(fmt.Errorf("field \"Policy.OrderConfirmedTemplate\" must be non-empty"))
	}

	if strings.TrimSpace(cfg.Policy.OrderCancelledTemplate) == "" {
		panic(fmt.Errorf("field \"Policy.OrderCancelledTemplate\" must be non-empty"))
	}

	validateRelay(cfg.Relay)

	return cfg
}

func validateRelay(relay Relay) {
	if relay.BatchSize < 1 {
		panic(fmt.Errorf("field \"Relay.BatchSize\" must be positive"))
	}

	if relay.Interval <= 0 {
		panic(fmt.Errorf("field \"Relay.Interval\" must be positive"))
	}

	if relay.RetryBaseBackoff <= 0 {
		panic(fmt.Errorf("field \"Relay.RetryBaseBackoff\" must be positive"))
	}

	if relay.RetryMaxBackoff <= 0 {
		panic(fmt.Errorf("field \"Relay.RetryMaxBackoff\" must be positive"))
	}

	if relay.RetryBaseBackoff > relay.RetryMaxBackoff {
		panic(fmt.Errorf("field \"Relay.RetryBaseBackoff\" must be less than or equal to Relay.RetryMaxBackoff"))
	}

	if strings.TrimSpace(relay.WorkerID) == "" {
		panic(fmt.Errorf("field \"Relay.WorkerID\" must be non-empty"))
	}

	if relay.StaleLockTTL <= 0 {
		panic(fmt.Errorf("field \"Relay.StaleLockTTL\" must be positive"))
	}
}

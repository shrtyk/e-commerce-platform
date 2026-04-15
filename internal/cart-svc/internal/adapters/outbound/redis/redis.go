package redis

import (
	"context"
	"fmt"

	redislib "github.com/redis/go-redis/v9"

	commoncfg "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

func MustCreateRedis(cfg commoncfg.Redis, timeouts commoncfg.Timeouts) *redislib.Client {
	client := redislib.NewClient(&redislib.Options{Addr: cfg.Addr})

	ctx, cancel := context.WithTimeout(context.Background(), timeouts.Startup)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		panic(fmt.Errorf("ping redis: %w", err))
	}

	return client
}

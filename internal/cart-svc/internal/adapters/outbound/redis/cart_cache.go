package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	redislib "github.com/redis/go-redis/v9"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
)

const activeCartCacheKeyPrefix = "cart:active:user:"

type CartCache struct {
	client *redislib.Client
}

func NewCartCache(client *redislib.Client) *CartCache {
	return &CartCache{client: client}
}

func (c *CartCache) GetActiveByUserID(ctx context.Context, userID uuid.UUID) (domain.Cart, bool, error) {
	payload, err := c.client.Get(ctx, activeCartCacheKey(userID)).Bytes()
	if err != nil {
		if errors.Is(err, redislib.Nil) {
			return domain.Cart{}, false, nil
		}

		return domain.Cart{}, false, fmt.Errorf("get active cart from redis: %w", err)
	}

	var cart domain.Cart
	if err := json.Unmarshal(payload, &cart); err != nil {
		return domain.Cart{}, false, fmt.Errorf("unmarshal active cart cache payload: %w", err)
	}

	return cart, true, nil
}

func (c *CartCache) SetActiveByUserID(ctx context.Context, userID uuid.UUID, cart domain.Cart, ttl time.Duration) error {
	payload, err := json.Marshal(cart)
	if err != nil {
		return fmt.Errorf("marshal active cart cache payload: %w", err)
	}

	if err := c.client.Set(ctx, activeCartCacheKey(userID), payload, ttl).Err(); err != nil {
		return fmt.Errorf("set active cart in redis: %w", err)
	}

	return nil
}

func (c *CartCache) DeleteActiveByUserID(ctx context.Context, userID uuid.UUID) error {
	if err := c.client.Del(ctx, activeCartCacheKey(userID)).Err(); err != nil {
		return fmt.Errorf("delete active cart from redis: %w", err)
	}

	return nil
}

func activeCartCacheKey(userID uuid.UUID) string {
	return activeCartCacheKeyPrefix + userID.String()
}

package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	redislib "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
)

func TestCartCacheRoundTrip(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redislib.NewClient(&redislib.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	cache := NewCartCache(client)
	userID := uuid.New()
	now := time.Now().UTC()

	item, err := domain.NewCartItem("SKU-1", "Product 1", 2, 1500, "USD", now, now)
	require.NoError(t, err)

	cart := domain.Cart{
		ID:          uuid.New(),
		UserID:      userID,
		Status:      domain.CartStatusActive,
		Currency:    "USD",
		Items:       []domain.CartItem{item},
		TotalAmount: 3000,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = cache.SetActiveByUserID(context.Background(), userID, cart, 2*time.Minute)
	require.NoError(t, err)

	got, found, err := cache.GetActiveByUserID(context.Background(), userID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, cart, got)
}

func TestCartCacheMiss(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redislib.NewClient(&redislib.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	cache := NewCartCache(client)

	got, found, err := cache.GetActiveByUserID(context.Background(), uuid.New())
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, domain.Cart{}, got)
}

func TestCartCacheDelete(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redislib.NewClient(&redislib.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	cache := NewCartCache(client)
	userID := uuid.New()
	cart := domain.Cart{UserID: userID, Status: domain.CartStatusActive, Items: []domain.CartItem{}}

	err := cache.SetActiveByUserID(context.Background(), userID, cart, time.Minute)
	require.NoError(t, err)

	err = cache.DeleteActiveByUserID(context.Background(), userID)
	require.NoError(t, err)

	got, found, err := cache.GetActiveByUserID(context.Background(), userID)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, domain.Cart{}, got)
}

package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/config"
)

func TestEnsureRelaySafeMode(t *testing.T) {
	t.Run("relay disabled is allowed", func(t *testing.T) {
		require.NotPanics(t, func() {
			ensureRelaySafeMode(config.Config{})
		})
	})

	t.Run("relay enabled panics without publisher wiring", func(t *testing.T) {
		require.PanicsWithValue(t, "outbox relay is enabled, but Kafka publisher is not configured in Gate B", func() {
			ensureRelaySafeMode(config.Config{Relay: config.Relay{Enabled: true}})
		})
	})
}

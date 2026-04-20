package provider

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/ports/outbound"
)

func TestStubProviderCharge(t *testing.T) {
	provider := NewStubProvider()

	t.Run("succeeds for even amount", func(t *testing.T) {
		result, err := provider.Charge(context.Background(), outbound.ChargePaymentInput{
			PaymentAttemptID: uuid.New(),
			OrderID:          uuid.New(),
			Amount:           100,
			Currency:         "USD",
			ProviderName:     "stub",
			IdempotencyKey:   "idem-1",
		})

		require.NoError(t, err)
		require.Equal(t, "stub-approved", result.ProviderReference)
	})

	t.Run("declines for odd amount", func(t *testing.T) {
		result, err := provider.Charge(context.Background(), outbound.ChargePaymentInput{
			PaymentAttemptID: uuid.New(),
			OrderID:          uuid.New(),
			Amount:           101,
			Currency:         "USD",
			ProviderName:     "stub",
			IdempotencyKey:   "idem-2",
		})

		require.ErrorIs(t, err, outbound.ErrPaymentDeclined)
		require.Equal(t, "declined", result.FailureCode)
		require.Equal(t, "stub decline", result.FailureMessage)
	})
}

package provider

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

func TestStubProviderSend(t *testing.T) {
	provider := NewStubProvider()

	t.Run("succeeds for regular recipient", func(t *testing.T) {
		deliveryRequestID := uuid.MustParse("6ee04980-4608-4d24-bbc2-68d3bba48fdb")

		result, err := provider.Send(context.Background(), outbound.SendDeliveryInput{
			DeliveryRequestID: deliveryRequestID,
			Channel:           "email",
			Recipient:         "user@example.com",
			TemplateKey:       "order-confirmed",
			Body:              "hello",
		})

		require.NoError(t, err)
		require.Equal(t, "stub-delivery", result.ProviderName)
		require.Equal(t, "stub-msg-6ee04980-4608-4d24-bbc2-68d3bba48fdb", result.ProviderMessageID)
		require.Empty(t, result.FailureCode)
		require.Empty(t, result.FailureMessage)
	})

	t.Run("returns deterministic failure for fail recipient", func(t *testing.T) {
		deliveryRequestID := uuid.MustParse("5eef7c71-c5fd-4a3f-b2a9-22ed9f8ce4b8")

		result, err := provider.Send(context.Background(), outbound.SendDeliveryInput{
			DeliveryRequestID: deliveryRequestID,
			Channel:           "email",
			Recipient:         "user@fail.test",
			TemplateKey:       "order-confirmed",
			Body:              "hello",
		})

		require.NoError(t, err)
		require.Equal(t, "stub-delivery", result.ProviderName)
		require.Equal(t, "stub-fail-5eef7c71-c5fd-4a3f-b2a9-22ed9f8ce4b8", result.ProviderMessageID)
		require.Equal(t, "recipient_suffix_fail", result.FailureCode)
		require.Equal(t, "recipient suffix @fail.test triggers deterministic failure", result.FailureMessage)
	})

	t.Run("returns wrapped validation error for invalid input", func(t *testing.T) {
		_, err := provider.Send(context.Background(), outbound.SendDeliveryInput{})

		require.Error(t, err)
		require.ErrorContains(t, err, "validate send delivery input")
		require.ErrorIs(t, err, outbound.ErrInvalidSendDeliveryArg)
	})
}

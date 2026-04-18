package checkout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

type checkoutIdempotencyPayloadStore interface {
	GetPayloadFingerprint(ctx context.Context, userID uuid.UUID, idempotencyKey string) (string, error)
}

type dbCheckoutIdempotencyGuard struct {
	store checkoutIdempotencyPayloadStore
}

func NewCheckoutIdempotencyGuard(store checkoutIdempotencyPayloadStore) outbound.CheckoutIdempotencyGuard {
	if store == nil {
		return noopCheckoutIdempotencyGuard{}
	}

	return dbCheckoutIdempotencyGuard{store: store}
}

func (g dbCheckoutIdempotencyGuard) ValidateCheckoutIdempotency(ctx context.Context, input outbound.ValidateCheckoutIdempotencyInput) error {
	expected, err := g.store.GetPayloadFingerprint(ctx, input.UserID, input.IdempotencyKey)
	if err != nil {
		return err
	}

	if expected != checkoutPayloadFingerprint(input.Payload) {
		return outbound.ErrCheckoutIdempotencyPayloadMismatch
	}

	return nil
}

func checkoutPayloadFingerprint(payload outbound.CheckoutIdempotencyPayload) string {
	normalizedPaymentMethod := strings.TrimSpace(strings.ToLower(payload.PaymentMethod))
	sum := sha256.Sum256([]byte("payment_method=" + normalizedPaymentMethod))
	return hex.EncodeToString(sum[:])
}

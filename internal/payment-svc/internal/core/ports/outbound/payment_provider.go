package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrPaymentDeclined = errors.New("payment declined")

type ChargePaymentInput struct {
	PaymentAttemptID uuid.UUID
	OrderID          uuid.UUID
	Amount           int64
	Currency         string
	ProviderName     string
	IdempotencyKey   string
}

type ChargePaymentResult struct {
	ProviderReference string
	FailureCode       string
	FailureMessage    string
}

//mockery:generate: true
type PaymentProvider interface {
	Charge(ctx context.Context, input ChargePaymentInput) (ChargePaymentResult, error)
}

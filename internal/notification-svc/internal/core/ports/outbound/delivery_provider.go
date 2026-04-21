package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrInvalidSendDeliveryArg = errors.New("notification invalid send delivery arg")

type SendDeliveryInput struct {
	DeliveryRequestID uuid.UUID
	Channel           string
	Recipient         string
	TemplateKey       string
	Body              string
}

type SendDeliveryResult struct {
	ProviderName      string
	ProviderMessageID string
	FailureCode       string
	FailureMessage    string
}

//mockery:generate: true
type DeliveryProvider interface {
	Send(ctx context.Context, input SendDeliveryInput) (SendDeliveryResult, error)
}

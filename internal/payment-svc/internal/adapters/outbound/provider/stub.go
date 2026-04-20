package provider

import (
	"context"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/ports/outbound"
)

type StubProvider struct{}

func NewStubProvider() *StubProvider {
	return &StubProvider{}
}

func (p *StubProvider) Charge(_ context.Context, input outbound.ChargePaymentInput) (outbound.ChargePaymentResult, error) {
	if input.Amount%2 == 0 {
		return outbound.ChargePaymentResult{ProviderReference: "stub-approved"}, nil
	}

	return outbound.ChargePaymentResult{
		FailureCode:    "declined",
		FailureMessage: "stub decline",
	}, outbound.ErrPaymentDeclined
}

var _ outbound.PaymentProvider = (*StubProvider)(nil)

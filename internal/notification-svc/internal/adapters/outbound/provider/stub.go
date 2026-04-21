package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

const (
	providerName               = "stub-delivery"
	failureRecipientSuffix     = "@fail.test"
	failureCodeRecipientSuffix = "recipient_suffix_fail"
	failureMessageRecipient    = "recipient suffix @fail.test triggers deterministic failure"
)

type StubProvider struct{}

func NewStubProvider() *StubProvider {
	return &StubProvider{}
}

func (p *StubProvider) Send(_ context.Context, input outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
	if err := validateSendDeliveryInput(input); err != nil {
		return outbound.SendDeliveryResult{}, fmt.Errorf("validate send delivery input: %w", err)
	}

	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(input.Recipient)), failureRecipientSuffix) {
		return outbound.SendDeliveryResult{
			ProviderName:      providerName,
			ProviderMessageID: "stub-fail-" + input.DeliveryRequestID.String(),
			FailureCode:       failureCodeRecipientSuffix,
			FailureMessage:    failureMessageRecipient,
		}, nil
	}

	return outbound.SendDeliveryResult{
		ProviderName:      providerName,
		ProviderMessageID: "stub-msg-" + input.DeliveryRequestID.String(),
	}, nil
}

func validateSendDeliveryInput(input outbound.SendDeliveryInput) error {
	if input.DeliveryRequestID == uuid.Nil ||
		strings.TrimSpace(input.Channel) == "" ||
		strings.TrimSpace(input.Recipient) == "" ||
		strings.TrimSpace(input.TemplateKey) == "" ||
		strings.TrimSpace(input.Body) == "" {
		return outbound.ErrInvalidSendDeliveryArg
	}

	return nil
}

var _ outbound.DeliveryProvider = (*StubProvider)(nil)

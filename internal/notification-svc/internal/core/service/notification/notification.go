package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

var (
	ErrInvalidRequestDeliveryInput = errors.New("notification invalid request delivery input")
	ErrInvalidMarkSentInput        = errors.New("notification invalid mark sent input")
	ErrInvalidMarkFailedInput      = errors.New("notification invalid mark failed input")
	ErrInvalidDeliveryTransition   = errors.New("notification invalid delivery transition")
	ErrDeliveryRequestNotFound     = errors.New("notification delivery request not found")
)

type RequestDeliveryInput struct {
	EventID           uuid.UUID
	ConsumerGroupName string
	SourceEventID     uuid.UUID
	SourceEventName   string
	Channel           string
	Recipient         string
	TemplateKey       string
	IdempotencyKey    string
}

type RequestDeliveryResult struct {
	DeliveryRequest  domain.DeliveryRequest
	IdempotentReplay bool
}

type MarkSentInput struct {
	EventID           uuid.UUID
	ConsumerGroupName string
	DeliveryRequestID uuid.UUID
	AttemptNumber     int32
	ProviderName      string
	ProviderMessageID string
	AttemptedAt       time.Time
}

type MarkSentResult struct {
	DeliveryRequest  domain.DeliveryRequest
	DeliveryAttempt  domain.DeliveryAttempt
	IdempotentReplay bool
}

type MarkFailedInput struct {
	EventID           uuid.UUID
	ConsumerGroupName string
	DeliveryRequestID uuid.UUID
	AttemptNumber     int32
	ProviderName      string
	ProviderMessageID string
	FailureCode       string
	FailureMessage    string
	AttemptedAt       time.Time
}

type MarkFailedResult struct {
	DeliveryRequest  domain.DeliveryRequest
	DeliveryAttempt  domain.DeliveryAttempt
	IdempotentReplay bool
}

func (s *NotificationService) RequestDelivery(
	ctx context.Context,
	input RequestDeliveryInput,
) (RequestDeliveryResult, error) {
	if err := validateRequestDeliveryInput(input); err != nil {
		return RequestDeliveryResult{}, err
	}

	idempotencyExists, err := s.consumerIdempotencies.Exists(ctx, input.EventID, input.ConsumerGroupName)
	if err != nil {
		return RequestDeliveryResult{}, fmt.Errorf("check consumer idempotency: %w", err)
	}
	if idempotencyExists {
		deliveryRequest, err := s.deliveryRequests.GetByIdempotencyKey(ctx, input.IdempotencyKey)
		if err != nil {
			return RequestDeliveryResult{}, fmt.Errorf("get delivery request by idempotency key: %w", mapDeliveryRequestErr(err, ErrInvalidRequestDeliveryInput))
		}

		return RequestDeliveryResult{
			DeliveryRequest:  deliveryRequest,
			IdempotentReplay: true,
		}, nil
	}

	deliveryRequest, err := s.deliveryRequests.CreateRequested(ctx, outbound.CreateDeliveryRequestInput{
		SourceEventID:   input.SourceEventID,
		SourceEventName: input.SourceEventName,
		Channel:         input.Channel,
		Recipient:       input.Recipient,
		TemplateKey:     input.TemplateKey,
		IdempotencyKey:  input.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, outbound.ErrDeliveryRequestDuplicate) {
			existingDeliveryRequest, existingErr := s.deliveryRequests.GetByIdempotencyKey(ctx, input.IdempotencyKey)
			if existingErr != nil {
				return RequestDeliveryResult{}, fmt.Errorf("get delivery request by idempotency key after duplicate: %w", mapDeliveryRequestErr(existingErr, ErrInvalidRequestDeliveryInput))
			}

			_, markerErr := s.createConsumerIdempotencyMarker(ctx, input.EventID, input.ConsumerGroupName, existingDeliveryRequest.DeliveryRequestID)
			if markerErr != nil {
				return RequestDeliveryResult{}, markerErr
			}

			return RequestDeliveryResult{
				DeliveryRequest:  existingDeliveryRequest,
				IdempotentReplay: true,
			}, nil
		}

		return RequestDeliveryResult{}, fmt.Errorf("create requested delivery request: %w", mapDeliveryRequestErr(err, ErrInvalidRequestDeliveryInput))
	}

	replay, err := s.createConsumerIdempotencyMarker(ctx, input.EventID, input.ConsumerGroupName, deliveryRequest.DeliveryRequestID)
	if err != nil {
		return RequestDeliveryResult{}, err
	}

	return RequestDeliveryResult{DeliveryRequest: deliveryRequest, IdempotentReplay: replay}, nil
}

func (s *NotificationService) MarkSent(ctx context.Context, input MarkSentInput) (MarkSentResult, error) {
	if err := validateMarkSentInput(input); err != nil {
		return MarkSentResult{}, err
	}

	replay, err := s.createConsumerIdempotencyMarker(ctx, input.EventID, input.ConsumerGroupName, input.DeliveryRequestID)
	if err != nil {
		return MarkSentResult{}, err
	}
	if replay {
		return MarkSentResult{IdempotentReplay: true}, nil
	}

	currentDeliveryRequest, err := s.deliveryRequests.GetByID(ctx, input.DeliveryRequestID)
	if err != nil {
		return MarkSentResult{}, fmt.Errorf("get delivery request by id: %w", mapDeliveryRequestErr(err, ErrInvalidMarkSentInput))
	}

	if currentDeliveryRequest.Status != domain.DeliveryStatusRequested {
		return MarkSentResult{}, fmt.Errorf("mark sent transition: %w", ErrInvalidDeliveryTransition)
	}

	deliveryAttempt, err := s.deliveryAttempts.Create(ctx, outbound.CreateDeliveryAttemptInput{
		DeliveryRequestID: input.DeliveryRequestID,
		AttemptNumber:     input.AttemptNumber,
		ProviderName:      input.ProviderName,
		ProviderMessageID: input.ProviderMessageID,
		AttemptedAt:       input.AttemptedAt,
	})
	if err != nil {
		return MarkSentResult{}, fmt.Errorf("create delivery attempt: %w", err)
	}

	updatedDeliveryRequest, err := s.deliveryRequests.MarkSent(ctx, input.DeliveryRequestID)
	if err != nil {
		return MarkSentResult{}, fmt.Errorf("mark delivery request sent: %w", mapDeliveryRequestErr(err, ErrInvalidMarkSentInput))
	}

	return MarkSentResult{
		DeliveryRequest:  updatedDeliveryRequest,
		DeliveryAttempt:  deliveryAttempt,
		IdempotentReplay: false,
	}, nil
}

func (s *NotificationService) MarkFailed(ctx context.Context, input MarkFailedInput) (MarkFailedResult, error) {
	if err := validateMarkFailedInput(input); err != nil {
		return MarkFailedResult{}, err
	}

	replay, err := s.createConsumerIdempotencyMarker(ctx, input.EventID, input.ConsumerGroupName, input.DeliveryRequestID)
	if err != nil {
		return MarkFailedResult{}, err
	}
	if replay {
		return MarkFailedResult{IdempotentReplay: true}, nil
	}

	currentDeliveryRequest, err := s.deliveryRequests.GetByID(ctx, input.DeliveryRequestID)
	if err != nil {
		return MarkFailedResult{}, fmt.Errorf("get delivery request by id: %w", mapDeliveryRequestErr(err, ErrInvalidMarkFailedInput))
	}

	if currentDeliveryRequest.Status != domain.DeliveryStatusRequested {
		return MarkFailedResult{}, fmt.Errorf("mark failed transition: %w", ErrInvalidDeliveryTransition)
	}

	deliveryAttempt, err := s.deliveryAttempts.Create(ctx, outbound.CreateDeliveryAttemptInput{
		DeliveryRequestID: input.DeliveryRequestID,
		AttemptNumber:     input.AttemptNumber,
		ProviderName:      input.ProviderName,
		ProviderMessageID: input.ProviderMessageID,
		FailureCode:       input.FailureCode,
		FailureMessage:    input.FailureMessage,
		AttemptedAt:       input.AttemptedAt,
	})
	if err != nil {
		return MarkFailedResult{}, fmt.Errorf("create delivery attempt: %w", err)
	}

	updatedDeliveryRequest, err := s.deliveryRequests.MarkFailed(ctx, input.DeliveryRequestID, input.FailureCode, input.FailureMessage)
	if err != nil {
		return MarkFailedResult{}, fmt.Errorf("mark delivery request failed: %w", mapDeliveryRequestErr(err, ErrInvalidMarkFailedInput))
	}

	return MarkFailedResult{
		DeliveryRequest:  updatedDeliveryRequest,
		DeliveryAttempt:  deliveryAttempt,
		IdempotentReplay: false,
	}, nil
}

func validateRequestDeliveryInput(input RequestDeliveryInput) error {
	if input.EventID == uuid.Nil ||
		strings.TrimSpace(input.ConsumerGroupName) == "" ||
		input.SourceEventID == uuid.Nil ||
		strings.TrimSpace(input.SourceEventName) == "" ||
		strings.TrimSpace(input.Channel) == "" ||
		strings.TrimSpace(input.Recipient) == "" ||
		strings.TrimSpace(input.TemplateKey) == "" ||
		strings.TrimSpace(input.IdempotencyKey) == "" {
		return ErrInvalidRequestDeliveryInput
	}

	return nil
}

func validateMarkSentInput(input MarkSentInput) error {
	if input.EventID == uuid.Nil ||
		strings.TrimSpace(input.ConsumerGroupName) == "" ||
		input.DeliveryRequestID == uuid.Nil ||
		input.AttemptNumber <= 0 ||
		strings.TrimSpace(input.ProviderName) == "" ||
		strings.TrimSpace(input.ProviderMessageID) == "" ||
		input.AttemptedAt.IsZero() {
		return ErrInvalidMarkSentInput
	}

	return nil
}

func validateMarkFailedInput(input MarkFailedInput) error {
	if input.EventID == uuid.Nil ||
		strings.TrimSpace(input.ConsumerGroupName) == "" ||
		input.DeliveryRequestID == uuid.Nil ||
		input.AttemptNumber <= 0 ||
		strings.TrimSpace(input.ProviderName) == "" ||
		strings.TrimSpace(input.ProviderMessageID) == "" ||
		strings.TrimSpace(input.FailureCode) == "" ||
		strings.TrimSpace(input.FailureMessage) == "" ||
		input.AttemptedAt.IsZero() {
		return ErrInvalidMarkFailedInput
	}

	return nil
}

func mapDeliveryRequestErr(err error, invalidInputErr error) error {
	if errors.Is(err, outbound.ErrDeliveryRequestNotFound) {
		return ErrDeliveryRequestNotFound
	}
	if errors.Is(err, outbound.ErrInvalidDeliveryRequestArg) {
		return invalidInputErr
	}

	return err
}

func (s *NotificationService) createConsumerIdempotencyMarker(
	ctx context.Context,
	eventID uuid.UUID,
	consumerGroupName string,
	deliveryRequestID uuid.UUID,
) (bool, error) {
	err := s.consumerIdempotencies.Create(ctx, outbound.CreateConsumerIdempotencyInput{
		EventID:           eventID,
		ConsumerGroupName: consumerGroupName,
		DeliveryRequestID: deliveryRequestID,
	})
	if err != nil {
		if errors.Is(err, outbound.ErrConsumerIdempotencyDuplicate) {
			return true, nil
		}

		return false, fmt.Errorf("create consumer idempotency: %w", err)
	}

	return false, nil
}

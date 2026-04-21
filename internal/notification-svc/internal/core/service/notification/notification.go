package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/common/tx"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

var (
	ErrInvalidRequestDeliveryInput  = errors.New("notification invalid request delivery input")
	ErrInvalidMarkSentInput         = errors.New("notification invalid mark sent input")
	ErrInvalidMarkFailedInput       = errors.New("notification invalid mark failed input")
	ErrInvalidHandleOrderEventInput = errors.New("notification invalid handle order event input")
	ErrInvalidDeliveryTransition    = errors.New("notification invalid delivery transition")
	ErrDeliveryRequestNotFound      = errors.New("notification delivery request not found")
)

const (
	defaultDeliveryResultsGroupSuffix = ".delivery-results"
	defaultProviderFailureCode        = "provider_error"
	defaultProviderFailureMessage     = "delivery provider failed"
	defaultProviderMessageID          = "unknown-provider-message-id"
)

type RequestDeliveryInput struct {
	EventID           uuid.UUID
	ConsumerGroupName string
	SourceEventID     uuid.UUID
	CorrelationID     string
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

type HandleOrderEventInput struct {
	EventID           uuid.UUID
	ConsumerGroupName string
	SourceEventID     uuid.UUID
	CorrelationID     string
	SourceEventName   string
	Channel           string
	Recipient         string
	TemplateKey       string
	Body              string
	AttemptedAt       time.Time
}

func (s *NotificationService) HandleOrderEvent(ctx context.Context, input HandleOrderEventInput) error {
	if err := validateHandleOrderEventInput(input); err != nil {
		return err
	}

	if s.deliveryProvider == nil {
		return fmt.Errorf("delivery provider is not configured")
	}

	requestResult, err := s.RequestDelivery(ctx, RequestDeliveryInput{
		EventID:           input.EventID,
		ConsumerGroupName: input.ConsumerGroupName,
		SourceEventID:     input.SourceEventID,
		CorrelationID:     input.CorrelationID,
		SourceEventName:   input.SourceEventName,
		Channel:           input.Channel,
		Recipient:         input.Recipient,
		TemplateKey:       input.TemplateKey,
		IdempotencyKey:    buildRequestIdempotencyKey(input.SourceEventName, input.EventID),
	})
	if err != nil {
		return fmt.Errorf("request delivery: %w", err)
	}

	if requestResult.IdempotentReplay && requestResult.DeliveryRequest.Status != domain.DeliveryStatusRequested {
		return nil
	}

	sendResult, sendErr := s.deliveryProvider.Send(ctx, outbound.SendDeliveryInput{
		DeliveryRequestID: requestResult.DeliveryRequest.DeliveryRequestID,
		Channel:           requestResult.DeliveryRequest.Channel,
		Recipient:         requestResult.DeliveryRequest.Recipient,
		TemplateKey:       requestResult.DeliveryRequest.TemplateKey,
		Body:              input.Body,
	})
	if sendErr != nil {
		return fmt.Errorf("send delivery: %w", sendErr)
	}

	failureCode := strings.TrimSpace(sendResult.FailureCode)
	failureMessage := strings.TrimSpace(sendResult.FailureMessage)
	providerMessageID := strings.TrimSpace(sendResult.ProviderMessageID)

	hasFailureDetails := failureCode != "" || failureMessage != ""

	if hasFailureDetails {
		providerMessageID := strings.TrimSpace(sendResult.ProviderMessageID)
		if providerMessageID == "" {
			providerMessageID = defaultProviderMessageID
		}

		_, markErr := s.MarkFailed(ctx, MarkFailedInput{
			EventID:           input.EventID,
			ConsumerGroupName: input.ConsumerGroupName + defaultDeliveryResultsGroupSuffix,
			DeliveryRequestID: requestResult.DeliveryRequest.DeliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      nonEmpty(strings.TrimSpace(sendResult.ProviderName), "delivery-provider"),
			ProviderMessageID: providerMessageID,
			FailureCode:       nonEmpty(failureCode, defaultProviderFailureCode),
			FailureMessage:    nonEmpty(failureMessage, defaultProviderFailureMessage),
			AttemptedAt:       input.AttemptedAt,
		})
		if markErr != nil {
			return fmt.Errorf("mark provider failure: %w", markErr)
		}

		return nil
	}

	_, err = s.MarkSent(ctx, MarkSentInput{
		EventID:           input.EventID,
		ConsumerGroupName: input.ConsumerGroupName + defaultDeliveryResultsGroupSuffix,
		DeliveryRequestID: requestResult.DeliveryRequest.DeliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      nonEmpty(strings.TrimSpace(sendResult.ProviderName), "unknown-provider"),
		ProviderMessageID: nonEmpty(providerMessageID, defaultProviderMessageID),
		AttemptedAt:       input.AttemptedAt,
	})
	if err != nil {
		return fmt.Errorf("mark provider sent: %w", err)
	}

	return nil
}

func (s *NotificationService) RequestDelivery(
	ctx context.Context,
	input RequestDeliveryInput,
) (RequestDeliveryResult, error) {
	if err := validateRequestDeliveryInput(input); err != nil {
		return RequestDeliveryResult{}, err
	}

	idempotencyExists, err := s.repos.ConsumerIdempotencies.Exists(ctx, input.EventID, input.ConsumerGroupName)
	if err != nil {
		return RequestDeliveryResult{}, fmt.Errorf("check consumer idempotency: %w", err)
	}
	if idempotencyExists {
		deliveryRequest, err := s.repos.DeliveryRequests.GetByIdempotencyKey(ctx, input.IdempotencyKey)
		if err != nil {
			return RequestDeliveryResult{}, fmt.Errorf("get delivery request by idempotency key: %w", mapDeliveryRequestErr(err, ErrInvalidRequestDeliveryInput))
		}

		return RequestDeliveryResult{
			DeliveryRequest:  deliveryRequest,
			IdempotentReplay: true,
		}, nil
	}

	var result RequestDeliveryResult
	err = s.doInTransaction(ctx, func(repos NotificationRepos) error {
		deliveryRequest, err := repos.DeliveryRequests.CreateRequested(ctx, outbound.CreateDeliveryRequestInput{
			SourceEventID:   input.SourceEventID,
			CorrelationID:   input.CorrelationID,
			SourceEventName: input.SourceEventName,
			Channel:         input.Channel,
			Recipient:       input.Recipient,
			TemplateKey:     input.TemplateKey,
			IdempotencyKey:  input.IdempotencyKey,
		})
		if err != nil {
			if errors.Is(err, outbound.ErrDeliveryRequestDuplicate) {
				existingDeliveryRequest, existingErr := repos.DeliveryRequests.GetByIdempotencyKey(ctx, input.IdempotencyKey)
				if existingErr != nil {
					return fmt.Errorf("get delivery request by idempotency key after duplicate: %w", mapDeliveryRequestErr(existingErr, ErrInvalidRequestDeliveryInput))
				}

				_, markerErr := s.createConsumerIdempotencyMarker(ctx, repos, input.EventID, input.ConsumerGroupName, existingDeliveryRequest.DeliveryRequestID)
				if markerErr != nil {
					return markerErr
				}

				result = RequestDeliveryResult{
					DeliveryRequest:  existingDeliveryRequest,
					IdempotentReplay: true,
				}

				return nil
			}

			return fmt.Errorf("create requested delivery request: %w", mapDeliveryRequestErr(err, ErrInvalidRequestDeliveryInput))
		}

		replay, err := s.createConsumerIdempotencyMarker(ctx, repos, input.EventID, input.ConsumerGroupName, deliveryRequest.DeliveryRequestID)
		if err != nil {
			return err
		}
		if replay {
			result = RequestDeliveryResult{DeliveryRequest: deliveryRequest, IdempotentReplay: true}
			return nil
		}

		if err := s.publishDeliveryRequested(ctx, repos, deliveryRequest, input.EventID); err != nil {
			return fmt.Errorf("publish delivery requested event: %w", err)
		}

		result = RequestDeliveryResult{DeliveryRequest: deliveryRequest, IdempotentReplay: false}

		return nil
	})
	if err != nil {
		return RequestDeliveryResult{}, err
	}

	return result, nil
}

func (s *NotificationService) MarkSent(ctx context.Context, input MarkSentInput) (MarkSentResult, error) {
	if err := validateMarkSentInput(input); err != nil {
		return MarkSentResult{}, err
	}

	var result MarkSentResult
	err := s.doInTransaction(ctx, func(repos NotificationRepos) error {
		replay, err := s.createConsumerIdempotencyMarker(ctx, repos, input.EventID, input.ConsumerGroupName, input.DeliveryRequestID)
		if err != nil {
			return err
		}
		if replay {
			result = MarkSentResult{IdempotentReplay: true}
			return nil
		}

		currentDeliveryRequest, err := repos.DeliveryRequests.GetByID(ctx, input.DeliveryRequestID)
		if err != nil {
			return fmt.Errorf("get delivery request by id: %w", mapDeliveryRequestErr(err, ErrInvalidMarkSentInput))
		}

		if currentDeliveryRequest.Status != domain.DeliveryStatusRequested {
			return fmt.Errorf("mark sent transition: %w", ErrInvalidDeliveryTransition)
		}

		deliveryAttempt, err := repos.DeliveryAttempts.Create(ctx, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: input.DeliveryRequestID,
			AttemptNumber:     input.AttemptNumber,
			ProviderName:      input.ProviderName,
			ProviderMessageID: input.ProviderMessageID,
			AttemptedAt:       input.AttemptedAt,
		})
		if err != nil {
			return fmt.Errorf("create delivery attempt: %w", err)
		}

		updatedDeliveryRequest, err := repos.DeliveryRequests.MarkSent(ctx, input.DeliveryRequestID)
		if err != nil {
			return fmt.Errorf("mark delivery request sent: %w", mapDeliveryRequestErr(err, ErrInvalidMarkSentInput))
		}

		if err := s.publishNotificationSent(ctx, repos, updatedDeliveryRequest, input.EventID); err != nil {
			return fmt.Errorf("publish notification sent event: %w", err)
		}

		result = MarkSentResult{
			DeliveryRequest:  updatedDeliveryRequest,
			DeliveryAttempt:  deliveryAttempt,
			IdempotentReplay: false,
		}

		return nil
	})
	if err != nil {
		return MarkSentResult{}, err
	}

	return result, nil
}

func (s *NotificationService) MarkFailed(ctx context.Context, input MarkFailedInput) (MarkFailedResult, error) {
	if err := validateMarkFailedInput(input); err != nil {
		return MarkFailedResult{}, err
	}

	var result MarkFailedResult
	err := s.doInTransaction(ctx, func(repos NotificationRepos) error {
		replay, err := s.createConsumerIdempotencyMarker(ctx, repos, input.EventID, input.ConsumerGroupName, input.DeliveryRequestID)
		if err != nil {
			return err
		}
		if replay {
			result = MarkFailedResult{IdempotentReplay: true}
			return nil
		}

		currentDeliveryRequest, err := repos.DeliveryRequests.GetByID(ctx, input.DeliveryRequestID)
		if err != nil {
			return fmt.Errorf("get delivery request by id: %w", mapDeliveryRequestErr(err, ErrInvalidMarkFailedInput))
		}

		if currentDeliveryRequest.Status != domain.DeliveryStatusRequested {
			return fmt.Errorf("mark failed transition: %w", ErrInvalidDeliveryTransition)
		}

		deliveryAttempt, err := repos.DeliveryAttempts.Create(ctx, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: input.DeliveryRequestID,
			AttemptNumber:     input.AttemptNumber,
			ProviderName:      input.ProviderName,
			ProviderMessageID: input.ProviderMessageID,
			FailureCode:       input.FailureCode,
			FailureMessage:    input.FailureMessage,
			AttemptedAt:       input.AttemptedAt,
		})
		if err != nil {
			return fmt.Errorf("create delivery attempt: %w", err)
		}

		updatedDeliveryRequest, err := repos.DeliveryRequests.MarkFailed(ctx, input.DeliveryRequestID, input.FailureCode, input.FailureMessage)
		if err != nil {
			return fmt.Errorf("mark delivery request failed: %w", mapDeliveryRequestErr(err, ErrInvalidMarkFailedInput))
		}

		if err := s.publishNotificationFailed(ctx, repos, updatedDeliveryRequest, input.EventID); err != nil {
			return fmt.Errorf("publish notification failed event: %w", err)
		}

		result = MarkFailedResult{
			DeliveryRequest:  updatedDeliveryRequest,
			DeliveryAttempt:  deliveryAttempt,
			IdempotentReplay: false,
		}

		return nil
	})
	if err != nil {
		return MarkFailedResult{}, err
	}

	return result, nil
}

func validateRequestDeliveryInput(input RequestDeliveryInput) error {
	if input.EventID == uuid.Nil ||
		strings.TrimSpace(input.ConsumerGroupName) == "" ||
		input.SourceEventID == uuid.Nil ||
		strings.TrimSpace(input.CorrelationID) == "" ||
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

func validateHandleOrderEventInput(input HandleOrderEventInput) error {
	if input.EventID == uuid.Nil ||
		strings.TrimSpace(input.ConsumerGroupName) == "" ||
		input.SourceEventID == uuid.Nil ||
		strings.TrimSpace(input.CorrelationID) == "" ||
		strings.TrimSpace(input.SourceEventName) == "" ||
		strings.TrimSpace(input.Channel) == "" ||
		strings.TrimSpace(input.Recipient) == "" ||
		strings.TrimSpace(input.TemplateKey) == "" ||
		strings.TrimSpace(input.Body) == "" ||
		input.AttemptedAt.IsZero() {
		return ErrInvalidHandleOrderEventInput
	}

	return nil
}

func buildRequestIdempotencyKey(sourceEvent string, eventID uuid.UUID) string {
	return sourceEvent + ":" + eventID.String()
}

func nonEmpty(value string, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
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
	repos NotificationRepos,
	eventID uuid.UUID,
	consumerGroupName string,
	deliveryRequestID uuid.UUID,
) (bool, error) {
	err := repos.ConsumerIdempotencies.Create(ctx, outbound.CreateConsumerIdempotencyInput{
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

func (s *NotificationService) doInTransaction(ctx context.Context, fn func(repos NotificationRepos) error) error {
	if s.txProvider == nil {
		return fn(s.repos)
	}

	return s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[NotificationRepos]) error {
		return fn(uow.Repos())
	})
}

package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
)

const (
	notificationEventsTopic          = "notification.events"
	deliveryRequestedEventName       = "notification.delivery_requested"
	notificationSentEventName        = "notification.sent"
	notificationFailedEventName      = "notification.failed"
	notificationAggregateType        = "delivery_request"
	notificationEventSchemaVersionV1 = "1"
)

func (s *NotificationService) publishDeliveryRequested(
	ctx context.Context,
	repos NotificationRepos,
	deliveryRequest domain.DeliveryRequest,
	causationEventID uuid.UUID,
) error {
	if repos.Publisher == nil {
		return nil
	}

	now := time.Now().UTC()

	return repos.Publisher.Publish(ctx, domain.DomainEvent{
		EventID:       uuid.NewString(),
		EventName:     deliveryRequestedEventName,
		Producer:      s.producer,
		OccurredAt:    now,
		CorrelationID: deliveryRequest.CorrelationID,
		CausationID:   causationEventID.String(),
		SchemaVersion: notificationEventSchemaVersionV1,
		AggregateType: notificationAggregateType,
		AggregateID:   deliveryRequest.DeliveryRequestID.String(),
		Topic:         notificationEventsTopic,
		Key:           deliveryRequest.DeliveryRequestID.String(),
		Payload: domain.DeliveryRequestedPayload{
			DeliveryRequestID: deliveryRequest.DeliveryRequestID.String(),
			SourceEventName:   deliveryRequest.SourceEventName,
			Channel:           deliveryRequest.Channel,
			Recipient:         deliveryRequest.Recipient,
			TemplateKey:       deliveryRequest.TemplateKey,
			Status:            deliveryRequest.Status,
		},
		Headers: map[string]string{"idempotencyKey": deliveryRequest.IdempotencyKey},
	})
}

func (s *NotificationService) publishNotificationSent(
	ctx context.Context,
	repos NotificationRepos,
	deliveryRequest domain.DeliveryRequest,
	causationEventID uuid.UUID,
) error {
	if repos.Publisher == nil {
		return nil
	}

	if deliveryRequest.Status != domain.DeliveryStatusSent {
		return fmt.Errorf("delivery request status must be sent")
	}

	now := time.Now().UTC()

	return repos.Publisher.Publish(ctx, domain.DomainEvent{
		EventID:       uuid.NewString(),
		EventName:     notificationSentEventName,
		Producer:      s.producer,
		OccurredAt:    now,
		CorrelationID: deliveryRequest.CorrelationID,
		CausationID:   causationEventID.String(),
		SchemaVersion: notificationEventSchemaVersionV1,
		AggregateType: notificationAggregateType,
		AggregateID:   deliveryRequest.DeliveryRequestID.String(),
		Topic:         notificationEventsTopic,
		Key:           deliveryRequest.DeliveryRequestID.String(),
		Payload: domain.NotificationSentPayload{
			DeliveryRequestID: deliveryRequest.DeliveryRequestID.String(),
			SourceEventName:   deliveryRequest.SourceEventName,
			Channel:           deliveryRequest.Channel,
			Recipient:         deliveryRequest.Recipient,
			Status:            deliveryRequest.Status,
			SentAt:            now,
		},
		Headers: map[string]string{"idempotencyKey": deliveryRequest.IdempotencyKey},
	})
}

func (s *NotificationService) publishNotificationFailed(
	ctx context.Context,
	repos NotificationRepos,
	deliveryRequest domain.DeliveryRequest,
	causationEventID uuid.UUID,
) error {
	if repos.Publisher == nil {
		return nil
	}

	if deliveryRequest.Status != domain.DeliveryStatusFailed {
		return fmt.Errorf("delivery request status must be failed")
	}

	now := time.Now().UTC()

	return repos.Publisher.Publish(ctx, domain.DomainEvent{
		EventID:       uuid.NewString(),
		EventName:     notificationFailedEventName,
		Producer:      s.producer,
		OccurredAt:    now,
		CorrelationID: deliveryRequest.CorrelationID,
		CausationID:   causationEventID.String(),
		SchemaVersion: notificationEventSchemaVersionV1,
		AggregateType: notificationAggregateType,
		AggregateID:   deliveryRequest.DeliveryRequestID.String(),
		Topic:         notificationEventsTopic,
		Key:           deliveryRequest.DeliveryRequestID.String(),
		Payload: domain.NotificationFailedPayload{
			DeliveryRequestID: deliveryRequest.DeliveryRequestID.String(),
			SourceEventName:   deliveryRequest.SourceEventName,
			Channel:           deliveryRequest.Channel,
			Recipient:         deliveryRequest.Recipient,
			Status:            deliveryRequest.Status,
			FailureCode:       deliveryRequest.LastErrorCode,
			FailureMessage:    deliveryRequest.LastErrorMessage,
			FailedAt:          now,
		},
		Headers: map[string]string{"idempotencyKey": deliveryRequest.IdempotencyKey},
	})
}

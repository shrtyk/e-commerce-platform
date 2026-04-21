package events

import (
	"context"
	"fmt"

	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	notificationv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/notification/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const deliveryRequestedEventName = "notification.delivery_requested"
const notificationSentEventName = "notification.sent"
const notificationFailedEventName = "notification.failed"

type outboxRepository interface {
	Append(ctx context.Context, record commonoutbox.Record) (commonoutbox.Record, error)
}

type EventPublisher struct {
	repository outboxRepository
}

func MustCreateOutboxEventPublisher(repository outboxRepository) *EventPublisher {
	publisher, err := NewEventPublisher(repository)
	if err != nil {
		panic(fmt.Errorf("create outbox event publisher: %w", err))
	}

	return publisher
}

func NewEventPublisher(repository outboxRepository) (*EventPublisher, error) {
	if repository == nil {
		return nil, fmt.Errorf("outbox repository is nil")
	}

	return &EventPublisher{repository: repository}, nil
}

func (p *EventPublisher) Publish(ctx context.Context, event domain.DomainEvent) error {
	if err := validateEnvelope(event); err != nil {
		return fmt.Errorf("validate event envelope: %w", err)
	}

	message, err := toProtoMessage(event)
	if err != nil {
		return fmt.Errorf("map domain event: %w", err)
	}

	payload, err := proto.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal protobuf payload: %w", err)
	}

	headers := commonkafka.MetadataToHeaders(commonkafka.EventMetadata{
		EventID:       event.EventID,
		EventName:     event.EventName,
		Producer:      event.Producer,
		OccurredAt:    event.OccurredAt,
		CorrelationID: event.CorrelationID,
		CausationID:   event.CausationID,
		SchemaVersion: event.SchemaVersion,
	}, event.Headers)
	headers[commonkafka.HeaderRecordName] = string(message.ProtoReflect().Descriptor().FullName())

	record := commonoutbox.Record{
		EventID:       event.EventID,
		EventName:     event.EventName,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		Topic:         event.Topic,
		Key:           []byte(event.Key),
		Payload:       payload,
		Headers:       headers,
		Status:        commonoutbox.StatusPending,
	}

	if _, err := p.repository.Append(ctx, record); err != nil {
		return fmt.Errorf("append outbox record: %w", err)
	}

	return nil
}

func validateEnvelope(event domain.DomainEvent) error {
	if event.EventID == "" {
		return fmt.Errorf("event_id is required")
	}

	if event.EventName == "" {
		return fmt.Errorf("event_name is required")
	}

	if event.Producer == "" {
		return fmt.Errorf("producer is required")
	}

	if event.OccurredAt.IsZero() {
		return fmt.Errorf("occurred_at is required")
	}

	if event.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}

	if event.Topic == "" {
		return fmt.Errorf("topic is required")
	}

	if event.Key == "" {
		return fmt.Errorf("key is required")
	}

	if event.Payload == nil {
		return fmt.Errorf("payload is required")
	}

	return nil
}

func toProtoMessage(event domain.DomainEvent) (proto.Message, error) {
	switch event.EventName {
	case deliveryRequestedEventName:
		payload, ok := event.Payload.(domain.DeliveryRequestedPayload)
		if !ok {
			return nil, fmt.Errorf("invalid delivery requested payload type %T", event.Payload)
		}

		return &notificationv1.DeliveryRequested{
			Metadata:          toEventMetadata(event),
			DeliveryRequestId: payload.DeliveryRequestID,
			SourceEventName:   payload.SourceEventName,
			Channel:           payload.Channel,
			Recipient:         payload.Recipient,
			TemplateKey:       payload.TemplateKey,
			Status:            toProtoDeliveryStatus(payload.Status),
		}, nil
	case notificationSentEventName:
		payload, ok := event.Payload.(domain.NotificationSentPayload)
		if !ok {
			return nil, fmt.Errorf("invalid notification sent payload type %T", event.Payload)
		}

		return &notificationv1.NotificationSent{
			Metadata:          toEventMetadata(event),
			DeliveryRequestId: payload.DeliveryRequestID,
			SourceEventName:   payload.SourceEventName,
			Channel:           payload.Channel,
			Recipient:         payload.Recipient,
			Status:            toProtoDeliveryStatus(payload.Status),
			SentAt:            timestamppb.New(payload.SentAt.UTC()),
		}, nil
	case notificationFailedEventName:
		payload, ok := event.Payload.(domain.NotificationFailedPayload)
		if !ok {
			return nil, fmt.Errorf("invalid notification failed payload type %T", event.Payload)
		}

		return &notificationv1.NotificationFailed{
			Metadata:          toEventMetadata(event),
			DeliveryRequestId: payload.DeliveryRequestID,
			SourceEventName:   payload.SourceEventName,
			Channel:           payload.Channel,
			Recipient:         payload.Recipient,
			Status:            toProtoDeliveryStatus(payload.Status),
			FailureCode:       payload.FailureCode,
			FailureMessage:    payload.FailureMessage,
			FailedAt:          timestamppb.New(payload.FailedAt.UTC()),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported event name: %s", event.EventName)
	}
}

func toEventMetadata(event domain.DomainEvent) *commonv1.EventMetadata {
	return &commonv1.EventMetadata{
		EventId:       event.EventID,
		EventName:     event.EventName,
		Producer:      event.Producer,
		OccurredAt:    timestamppb.New(event.OccurredAt.UTC()),
		CorrelationId: event.CorrelationID,
		CausationId:   event.CausationID,
		SchemaVersion: event.SchemaVersion,
	}
}

func toProtoDeliveryStatus(status domain.DeliveryStatus) notificationv1.DeliveryStatus {
	switch status {
	case domain.DeliveryStatusRequested:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_REQUESTED
	case domain.DeliveryStatusSent:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT
	case domain.DeliveryStatusFailed:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_FAILED
	default:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED
	}
}

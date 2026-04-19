package events

import (
	"context"
	"fmt"

	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const paymentInitiatedEventName = "payment.initiated"

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
	case paymentInitiatedEventName:
		payload, ok := event.Payload.(domain.PaymentInitiatedPayload)
		if !ok {
			return nil, fmt.Errorf("invalid payment initiated payload type %T", event.Payload)
		}

		return &paymentv1.PaymentInitiated{
			Metadata: &commonv1.EventMetadata{
				EventId:       event.EventID,
				EventName:     event.EventName,
				Producer:      event.Producer,
				OccurredAt:    timestamppb.New(event.OccurredAt.UTC()),
				CorrelationId: event.CorrelationID,
				CausationId:   event.CausationID,
				SchemaVersion: event.SchemaVersion,
			},
			PaymentAttemptId: payload.PaymentAttemptID,
			OrderId:          payload.OrderID,
			Status:           toProtoPaymentStatus(payload.Status),
			Amount:           &commonv1.Money{Amount: payload.Amount, Currency: payload.Currency},
			ProviderName:     payload.ProviderName,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported event name: %s", event.EventName)
	}
}

func toProtoPaymentStatus(status domain.PaymentStatus) paymentv1.PaymentStatus {
	switch status {
	case domain.PaymentStatusInitiated:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_INITIATED
	case domain.PaymentStatusProcessing:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_PROCESSING
	case domain.PaymentStatusSucceeded:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_SUCCEEDED
	case domain.PaymentStatusFailed:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_FAILED
	default:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_UNSPECIFIED
	}
}

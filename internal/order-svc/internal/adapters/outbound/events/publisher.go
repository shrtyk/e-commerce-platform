package events

import (
	"context"
	"fmt"

	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/domain"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	orderCreatedEventName   = "order.created"
	orderConfirmedEventName = "order.confirmed"
	orderCancelledEventName = "order.cancelled"
)

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
	case orderCreatedEventName:
		payload, ok := event.Payload.(domain.OrderCreatedPayload)
		if !ok {
			return nil, fmt.Errorf("invalid order created payload type %T", event.Payload)
		}

		items := make([]*orderv1.OrderItemSnapshot, 0, len(payload.Items))
		for _, item := range payload.Items {
			items = append(items, &orderv1.OrderItemSnapshot{
				ProductId: item.ProductID,
				Sku:       item.SKU,
				Name:      item.Name,
				Quantity:  int64(item.Quantity),
				UnitPrice: &commonv1.Money{Amount: item.UnitPrice, Currency: item.Currency},
				LineTotal: &commonv1.Money{Amount: item.LineTotal, Currency: item.Currency},
			})
		}

		return &orderv1.OrderCreated{
			Metadata: mapMetadata(event),
			OrderId:  payload.OrderID,
			UserId:   payload.UserID,
			Status:   toProtoOrderStatus(payload.Status),
			Currency: payload.Currency,
			TotalAmount: &commonv1.Money{
				Amount:   payload.TotalAmount,
				Currency: payload.Currency,
			},
			Items: items,
		}, nil
	case orderCancelledEventName:
		payload, ok := event.Payload.(domain.OrderCancelledPayload)
		if !ok {
			return nil, fmt.Errorf("invalid order cancelled payload type %T", event.Payload)
		}

		return &orderv1.OrderCancelled{
			Metadata:            mapMetadata(event),
			OrderId:             payload.OrderID,
			UserId:              payload.UserID,
			Status:              toProtoOrderStatus(payload.Status),
			CancelReasonCode:    payload.CancelReasonCode,
			CancelReasonMessage: payload.CancelReasonMessage,
			CancelledAt:         timestamppb.New(payload.CancelledAt.UTC()),
		}, nil
	case orderConfirmedEventName:
		payload, ok := event.Payload.(domain.OrderConfirmedPayload)
		if !ok {
			return nil, fmt.Errorf("invalid order confirmed payload type %T", event.Payload)
		}

		return &orderv1.OrderConfirmed{
			Metadata:  mapMetadata(event),
			OrderId:   payload.OrderID,
			UserId:    payload.UserID,
			Status:    toProtoOrderStatus(payload.Status),
			Currency:  payload.Currency,
			ConfirmedAt: timestamppb.New(payload.ConfirmedAt.UTC()),
			TotalAmount: &commonv1.Money{
				Amount:   payload.TotalAmount,
				Currency: payload.Currency,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported event name: %s", event.EventName)
	}
}

func mapMetadata(event domain.DomainEvent) *commonv1.EventMetadata {
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

func toProtoOrderStatus(status domain.OrderStatus) orderv1.OrderStatus {
	switch status {
	case domain.OrderStatusPending:
		return orderv1.OrderStatus_ORDER_STATUS_PENDING
	case domain.OrderStatusAwaitingStock:
		return orderv1.OrderStatus_ORDER_STATUS_AWAITING_STOCK
	case domain.OrderStatusAwaitingPayment:
		return orderv1.OrderStatus_ORDER_STATUS_AWAITING_PAYMENT
	case domain.OrderStatusConfirmed:
		return orderv1.OrderStatus_ORDER_STATUS_CONFIRMED
	case domain.OrderStatusCancelled:
		return orderv1.OrderStatus_ORDER_STATUS_CANCELLED
	default:
		return orderv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

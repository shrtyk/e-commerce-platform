package events

import (
	"context"
	"errors"
	"testing"
	"time"

	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/domain"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type fakeOutboxRepository struct {
	err    error
	record commonoutbox.Record
}

func (f *fakeOutboxRepository) Append(_ context.Context, record commonoutbox.Record) (commonoutbox.Record, error) {
	f.record = record

	if f.err != nil {
		return commonoutbox.Record{}, f.err
	}

	return record, nil
}

func TestEventPublisherPublish(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name         string
		event        domain.DomainEvent
		publisherErr error
		errContains  string
	}{
		{
			name: "order created success",
			event: domain.DomainEvent{
				EventID:       "8cf98bbc-c5da-458d-842b-d89f1971f586",
				EventName:     "order.created",
				Producer:      "order-svc",
				OccurredAt:    now,
				CorrelationID: "corr-1",
				CausationID:   "idem-1",
				SchemaVersion: "1",
				AggregateType: "order",
				AggregateID:   "order-1",
				Topic:         "order.events",
				Key:           "order-1",
				Payload: domain.OrderCreatedPayload{
					OrderID:     "order-1",
					UserID:      "user-1",
					Status:      domain.OrderStatusPending,
					Currency:    "USD",
					TotalAmount: 1500,
					Items: []domain.OrderItemSnapshot{{
						ProductID: "product-1",
						SKU:       "SKU-1",
						Name:      "Item 1",
						Quantity:  2,
						UnitPrice: 500,
						LineTotal: 1000,
						Currency:  "USD",
					}},
				},
				Headers: map[string]string{"idempotencyKey": "idem-1"},
			},
		},
		{
			name: "order cancelled success",
			event: domain.DomainEvent{
				EventID:       "370f0566-3ca0-4147-bf2a-865ff1756118",
				EventName:     "order.cancelled",
				Producer:      "order-svc",
				OccurredAt:    now,
				CorrelationID: "corr-2",
				CausationID:   "payment_declined",
				SchemaVersion: "1",
				AggregateType: "order",
				AggregateID:   "order-2",
				Topic:         "order.events",
				Key:           "order-2",
				Payload: domain.OrderCancelledPayload{
					OrderID:             "order-2",
					UserID:              "user-2",
					Status:              domain.OrderStatusCancelled,
					CancelReasonCode:    "payment_declined",
					CancelReasonMessage: "payment_declined",
					CancelledAt:         now,
				},
			},
		},
		{
			name: "unsupported event",
			event: domain.DomainEvent{
				EventID:       "fd6f1817-fd78-4e7f-a126-a97810c4259b",
				EventName:     "order.unknown",
				Producer:      "order-svc",
				OccurredAt:    now,
				SchemaVersion: "1",
				AggregateType: "order",
				AggregateID:   "order-3",
				Topic:         "order.events",
				Key:           "order-3",
				Payload:       domain.OrderCreatedPayload{},
			},
			errContains: "unsupported event name",
		},
		{
			name: "append error",
			event: domain.DomainEvent{
				EventID:       "526cda33-c4ff-4738-9ce0-8f0980ccba5d",
				EventName:     "order.created",
				Producer:      "order-svc",
				OccurredAt:    now,
				SchemaVersion: "1",
				AggregateType: "order",
				AggregateID:   "order-4",
				Topic:         "order.events",
				Key:           "order-4",
				Payload: domain.OrderCreatedPayload{
					OrderID:     "order-4",
					UserID:      "user-4",
					Status:      domain.OrderStatusPending,
					Currency:    "USD",
					TotalAmount: 100,
				},
			},
			publisherErr: errors.New("db down"),
			errContains:  "append outbox record",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeOutboxRepository{err: tt.publisherErr}
			publisher, err := NewEventPublisher(fake)
			require.NoError(t, err)

			err = publisher.Publish(context.Background(), tt.event)
			if tt.errContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, commonoutbox.StatusPending, fake.record.Status)
			require.Equal(t, tt.event.EventID, fake.record.Headers[commonkafka.HeaderEventID])
			require.Equal(t, tt.event.EventName, fake.record.Headers[commonkafka.HeaderEventName])
			require.Equal(t, tt.event.Producer, fake.record.Headers[commonkafka.HeaderProducer])

			switch tt.event.EventName {
			case "order.created":
				var payload orderv1.OrderCreated
				err = proto.Unmarshal(fake.record.Payload, &payload)
				require.NoError(t, err)
				require.Equal(t, orderv1.OrderStatus_ORDER_STATUS_PENDING, payload.Status)
				require.Equal(t, "ecommerce.order.v1.OrderCreated", fake.record.Headers[commonkafka.HeaderRecordName])
			case "order.cancelled":
				var payload orderv1.OrderCancelled
				err = proto.Unmarshal(fake.record.Payload, &payload)
				require.NoError(t, err)
				require.Equal(t, orderv1.OrderStatus_ORDER_STATUS_CANCELLED, payload.Status)
				require.Equal(t, "ecommerce.order.v1.OrderCancelled", fake.record.Headers[commonkafka.HeaderRecordName])
			}
		})
	}
}

func TestEventPublisherPublishEnvelopeValidation(t *testing.T) {
	tests := []struct {
		name        string
		event       domain.DomainEvent
		errContains string
	}{
		{name: "missing event_id", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.EventID = "" }), errContains: "event_id is required"},
		{name: "missing event_name", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.EventName = "" }), errContains: "event_name is required"},
		{name: "missing producer", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Producer = "" }), errContains: "producer is required"},
		{name: "missing occurred_at", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.OccurredAt = time.Time{} }), errContains: "occurred_at is required"},
		{name: "missing schema_version", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.SchemaVersion = "" }), errContains: "schema_version is required"},
		{name: "missing topic", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Topic = "" }), errContains: "topic is required"},
		{name: "missing key", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Key = "" }), errContains: "key is required"},
		{name: "missing payload", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Payload = nil }), errContains: "payload is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeOutboxRepository{}
			publisher, err := NewEventPublisher(fake)
			require.NoError(t, err)

			err = publisher.Publish(context.Background(), tt.event)
			require.Error(t, err)
			require.ErrorContains(t, err, "validate event envelope")
			require.ErrorContains(t, err, tt.errContains)
			require.Empty(t, fake.record)
		})
	}
}

func validEventForValidation() domain.DomainEvent {
	return domain.DomainEvent{
		EventID:       "8cf98bbc-c5da-458d-842b-d89f1971f586",
		EventName:     "order.created",
		Producer:      "order-svc",
		OccurredAt:    time.Now().UTC(),
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		SchemaVersion: "1",
		Topic:         "order.events",
		Key:           "order-1",
		AggregateType: "order",
		AggregateID:   "order-1",
		Payload: domain.OrderCreatedPayload{
			OrderID:     "order-1",
			UserID:      "user-1",
			Status:      domain.OrderStatusPending,
			Currency:    "USD",
			TotalAmount: 100,
		},
	}
}

func withEvent(event domain.DomainEvent, mutate func(event *domain.DomainEvent)) domain.DomainEvent {
	mutate(&event)
	return event
}

func TestNewEventPublisher(t *testing.T) {
	_, err := NewEventPublisher(nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "outbox repository is nil")
}

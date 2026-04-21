package events

import (
	"context"
	"errors"
	"testing"
	"time"

	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"github.com/stretchr/testify/require"
	notificationpb "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/notification/v1"
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

func TestEventPublisherPublishRequested(t *testing.T) {
	fake := &fakeOutboxRepository{}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	now := time.Now().UTC()
	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "evt-1",
		EventName:     "notification.delivery_requested",
		Producer:      "notification-svc",
		OccurredAt:    now,
		CorrelationID: "order-1",
		CausationID:   "cause-1",
		SchemaVersion: "1",
		AggregateType: "delivery_request",
		AggregateID:   "req-1",
		Topic:         "notification.events",
		Key:           "req-1",
		Payload: domain.DeliveryRequestedPayload{
			DeliveryRequestID: "req-1",
			SourceEventName:   "order.confirmed",
			Channel:           "in_app",
			Recipient:         "user-1",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
		},
	})
	require.NoError(t, err)
	require.Equal(t, commonoutbox.StatusPending, fake.record.Status)

	var payload notificationpb.DeliveryRequested
	err = proto.Unmarshal(fake.record.Payload, &payload)
	require.NoError(t, err)
	require.Equal(t, "req-1", payload.DeliveryRequestId)
	require.Equal(t, notificationpb.DeliveryStatus_DELIVERY_STATUS_REQUESTED, payload.Status)
	require.Equal(t, "evt-1", fake.record.Headers[commonkafka.HeaderEventID])
	require.Equal(t, "notification.delivery_requested", fake.record.Headers[commonkafka.HeaderEventName])
	require.Equal(t, "ecommerce.notification.v1.DeliveryRequested", fake.record.Headers[commonkafka.HeaderRecordName])
}

func TestEventPublisherPublishFailed(t *testing.T) {
	fake := &fakeOutboxRepository{}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	now := time.Now().UTC()
	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "evt-2",
		EventName:     "notification.failed",
		Producer:      "notification-svc",
		OccurredAt:    now,
		CorrelationID: "order-1",
		CausationID:   "cause-2",
		SchemaVersion: "1",
		AggregateType: "delivery_request",
		AggregateID:   "req-2",
		Topic:         "notification.events",
		Key:           "req-2",
		Payload: domain.NotificationFailedPayload{
			DeliveryRequestID: "req-2",
			SourceEventName:   "order.cancelled",
			Channel:           "in_app",
			Recipient:         "user-2",
			Status:            domain.DeliveryStatusFailed,
			FailureCode:       "provider-timeout",
			FailureMessage:    "provider timeout",
			FailedAt:          now,
		},
	})
	require.NoError(t, err)

	var payload notificationpb.NotificationFailed
	err = proto.Unmarshal(fake.record.Payload, &payload)
	require.NoError(t, err)
	require.Equal(t, "provider-timeout", payload.FailureCode)
	require.Equal(t, notificationpb.DeliveryStatus_DELIVERY_STATUS_FAILED, payload.Status)
	require.Equal(t, "ecommerce.notification.v1.NotificationFailed", fake.record.Headers[commonkafka.HeaderRecordName])
}

func TestEventPublisherPublishErrors(t *testing.T) {
	fake := &fakeOutboxRepository{}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), domain.DomainEvent{})
	require.Error(t, err)
	require.ErrorContains(t, err, "validate event envelope")

	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "evt",
		EventName:     "notification.sent",
		Producer:      "notification-svc",
		OccurredAt:    time.Now().UTC(),
		CorrelationID: "order-1",
		CausationID:   "cause",
		SchemaVersion: "1",
		AggregateType: "delivery_request",
		AggregateID:   "req",
		Topic:         "notification.events",
		Key:           "req",
		Payload:       "bad",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid notification sent payload type")

	fake.err = errors.New("append boom")
	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "evt-3",
		EventName:     "notification.sent",
		Producer:      "notification-svc",
		OccurredAt:    time.Now().UTC(),
		CorrelationID: "order-1",
		CausationID:   "cause-3",
		SchemaVersion: "1",
		AggregateType: "delivery_request",
		AggregateID:   "req-3",
		Topic:         "notification.events",
		Key:           "req-3",
		Payload: domain.NotificationSentPayload{
			DeliveryRequestID: "req-3",
			SourceEventName:   "order.confirmed",
			Channel:           "in_app",
			Recipient:         "user-3",
			Status:            domain.DeliveryStatusSent,
			SentAt:            time.Now().UTC(),
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "append outbox record")
}

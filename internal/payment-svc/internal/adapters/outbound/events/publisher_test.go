package events

import (
	"context"
	"errors"
	"testing"
	"time"

	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
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

	fake := &fakeOutboxRepository{}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "8cf98bbc-c5da-458d-842b-d89f1971f586",
		EventName:     "payment.initiated",
		Producer:      "payment-svc",
		OccurredAt:    now,
		CorrelationID: "corr-1",
		CausationID:   "idem-1",
		SchemaVersion: "1",
		AggregateType: "payment_attempt",
		AggregateID:   "pa-1",
		Topic:         "payment.events",
		Key:           "pa-1",
		Payload: domain.PaymentInitiatedPayload{
			PaymentAttemptID: "pa-1",
			OrderID:          "order-1",
			Status:           domain.PaymentStatusInitiated,
			Amount:           1500,
			Currency:         "USD",
			ProviderName:     "stub",
		},
		Headers: map[string]string{"idempotencyKey": "idem-1"},
	})
	require.NoError(t, err)

	require.Equal(t, commonoutbox.StatusPending, fake.record.Status)
	require.Equal(t, "8cf98bbc-c5da-458d-842b-d89f1971f586", fake.record.Headers[commonkafka.HeaderEventID])
	require.Equal(t, "payment.initiated", fake.record.Headers[commonkafka.HeaderEventName])
	require.Equal(t, "payment-svc", fake.record.Headers[commonkafka.HeaderProducer])
	require.Equal(t, "ecommerce.payment.v1.PaymentInitiated", fake.record.Headers[commonkafka.HeaderRecordName])

	var payload paymentv1.PaymentInitiated
	err = proto.Unmarshal(fake.record.Payload, &payload)
	require.NoError(t, err)
	require.Equal(t, "pa-1", payload.GetPaymentAttemptId())
	require.Equal(t, "order-1", payload.GetOrderId())
	require.Equal(t, paymentv1.PaymentStatus_PAYMENT_STATUS_INITIATED, payload.GetStatus())
}

func TestEventPublisherPublishPaymentSucceeded(t *testing.T) {
	now := time.Now().UTC()

	fake := &fakeOutboxRepository{}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "9cf98bbc-c5da-458d-842b-d89f1971f586",
		EventName:     "payment.succeeded",
		Producer:      "payment-svc",
		OccurredAt:    now,
		CorrelationID: "corr-1",
		CausationID:   "idem-1",
		SchemaVersion: "1",
		AggregateType: "payment_attempt",
		AggregateID:   "pa-2",
		Topic:         "payment.events",
		Key:           "pa-2",
		Payload: domain.PaymentSucceededPayload{
			PaymentAttemptID:  "pa-2",
			OrderID:           "order-2",
			Status:            domain.PaymentStatusSucceeded,
			Amount:            2500,
			Currency:          "USD",
			ProviderName:      "stub",
			ProviderReference: "ref-1",
			ProcessedAt:       &now,
		},
	})
	require.NoError(t, err)

	var payload paymentv1.PaymentSucceeded
	err = proto.Unmarshal(fake.record.Payload, &payload)
	require.NoError(t, err)
	require.Equal(t, "pa-2", payload.GetPaymentAttemptId())
	require.Equal(t, "order-2", payload.GetOrderId())
	require.Equal(t, paymentv1.PaymentStatus_PAYMENT_STATUS_SUCCEEDED, payload.GetStatus())
	require.Equal(t, "ref-1", payload.GetProviderReference())
	require.NotNil(t, payload.GetProcessedAt())
	require.Equal(t, now.Unix(), payload.GetProcessedAt().AsTime().Unix())
	require.Equal(t, "ecommerce.payment.v1.PaymentSucceeded", fake.record.Headers[commonkafka.HeaderRecordName])
}

func TestEventPublisherPublishPaymentFailed(t *testing.T) {
	now := time.Now().UTC()

	fake := &fakeOutboxRepository{}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "acf98bbc-c5da-458d-842b-d89f1971f586",
		EventName:     "payment.failed",
		Producer:      "payment-svc",
		OccurredAt:    now,
		CorrelationID: "corr-1",
		CausationID:   "idem-1",
		SchemaVersion: "1",
		AggregateType: "payment_attempt",
		AggregateID:   "pa-3",
		Topic:         "payment.events",
		Key:           "pa-3",
		Payload: domain.PaymentFailedPayload{
			PaymentAttemptID: "pa-3",
			OrderID:          "order-3",
			Status:           domain.PaymentStatusFailed,
			Amount:           3500,
			Currency:         "USD",
			ProviderName:     "stub",
			FailureCode:      "declined",
			FailureMessage:   "insufficient funds",
			ProcessedAt:      &now,
		},
	})
	require.NoError(t, err)

	var payload paymentv1.PaymentFailed
	err = proto.Unmarshal(fake.record.Payload, &payload)
	require.NoError(t, err)
	require.Equal(t, "pa-3", payload.GetPaymentAttemptId())
	require.Equal(t, "order-3", payload.GetOrderId())
	require.Equal(t, paymentv1.PaymentStatus_PAYMENT_STATUS_FAILED, payload.GetStatus())
	require.Equal(t, "declined", payload.GetFailureCode())
	require.Equal(t, "insufficient funds", payload.GetFailureMessage())
	require.NotNil(t, payload.GetProcessedAt())
	require.Equal(t, now.Unix(), payload.GetProcessedAt().AsTime().Unix())
	require.Equal(t, "ecommerce.payment.v1.PaymentFailed", fake.record.Headers[commonkafka.HeaderRecordName])
}

func TestEventPublisherPublishReturnsErrorOnInvalidPayload(t *testing.T) {
	fake := &fakeOutboxRepository{}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "8cf98bbc-c5da-458d-842b-d89f1971f586",
		EventName:     "payment.initiated",
		Producer:      "payment-svc",
		OccurredAt:    time.Now().UTC(),
		SchemaVersion: "1",
		AggregateType: "payment_attempt",
		AggregateID:   "pa-1",
		Topic:         "payment.events",
		Key:           "pa-1",
		Payload:       "wrong-type",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "map domain event")
	require.ErrorContains(t, err, "invalid payment initiated payload type")
}

func TestEventPublisherPublishWrapsAppendError(t *testing.T) {
	fake := &fakeOutboxRepository{err: errors.New("db down")}
	publisher, err := NewEventPublisher(fake)
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), domain.DomainEvent{
		EventID:       "8cf98bbc-c5da-458d-842b-d89f1971f586",
		EventName:     "payment.initiated",
		Producer:      "payment-svc",
		OccurredAt:    time.Now().UTC(),
		SchemaVersion: "1",
		AggregateType: "payment_attempt",
		AggregateID:   "pa-1",
		Topic:         "payment.events",
		Key:           "pa-1",
		Payload: domain.PaymentInitiatedPayload{
			PaymentAttemptID: "pa-1",
			OrderID:          "order-1",
			Status:           domain.PaymentStatusInitiated,
			Amount:           1500,
			Currency:         "USD",
			ProviderName:     "stub",
		},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "append outbox record")
}

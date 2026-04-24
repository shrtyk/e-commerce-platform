package kafka

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/service/notification"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type orderEventsConsumerStub struct {
	pollFunc   func(ctx context.Context) ([]commonkafka.ConsumedMessage, error)
	commitFunc func(ctx context.Context) error
}

func (s orderEventsConsumerStub) Poll(ctx context.Context) ([]commonkafka.ConsumedMessage, error) {
	if s.pollFunc == nil {
		return nil, nil
	}

	return s.pollFunc(ctx)
}

func (s orderEventsConsumerStub) CommitUncommittedOffsets(ctx context.Context) error {
	if s.commitFunc == nil {
		return nil
	}

	return s.commitFunc(ctx)
}

type notificationServiceStub struct {
	handleOrderEventFunc func(ctx context.Context, input notification.HandleOrderEventInput) error
}

type orderEventsPublisherStub struct {
	publishFunc func(ctx context.Context, envelope commonkafka.EventEnvelope) error
}

func (s orderEventsPublisherStub) Publish(ctx context.Context, envelope commonkafka.EventEnvelope) error {
	if s.publishFunc == nil {
		return nil
	}

	return s.publishFunc(ctx, envelope)
}

func (s notificationServiceStub) HandleOrderEvent(ctx context.Context, input notification.HandleOrderEventInput) error {
	if s.handleOrderEventFunc == nil {
		return nil
	}

	return s.handleOrderEventFunc(ctx, input)
}

func TestOrderEventsWorkerTickHandlesOrderConfirmed(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()
	orderID := uuid.New()
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	configuredChannel := "email"
	confirmedTemplate := "custom confirm for %s"

	calledRequest := false
	commitCalls := 0

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: newOrderConfirmedEvent(eventID.String(), orderID.String(), "user-1"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, input notification.HandleOrderEventInput) error {
				calledRequest = true
				require.Equal(t, eventID, input.EventID)
				require.Equal(t, eventID.String(), input.CorrelationID)
				require.Equal(t, "notification-svc-order-events-v1", input.ConsumerGroupName)
				require.Equal(t, "order.confirmed", input.SourceEventName)
				require.Equal(t, configuredChannel, input.Channel)
				require.Equal(t, "user-1", input.Recipient)
				require.Equal(t, "order-confirmed", input.TemplateKey)
				require.Equal(t, "custom confirm for "+orderID.String(), input.Body)
				require.Equal(t, now, input.AttemptedAt)
				return nil
			},
		},
		orderEventsPublisherStub{},
		OrderEventsWorkerConfig{
			PollInterval:      time.Millisecond,
			ConsumerGroupName: "notification-svc-order-events-v1",
			MaxRetryAttempts:  3,
			DefaultChannel:    configuredChannel,
			ConfirmedTemplate: confirmedTemplate,
			CancelledTemplate: "unused %s %s",
		},
	)
	require.NoError(t, err)
	worker.now = func() time.Time { return now }

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, calledRequest)
	require.Equal(t, 1, commitCalls)
}

func TestNewOrderEventsWorkerRejectsBlankPolicyConfig(t *testing.T) {
	t.Parallel()

	_, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{},
		notificationServiceStub{},
		orderEventsPublisherStub{},
		OrderEventsWorkerConfig{
			PollInterval:      time.Millisecond,
			ConsumerGroupName: "notification-svc-order-events-v1",
			MaxRetryAttempts:  3,
			DefaultChannel:    " ",
			ConfirmedTemplate: "",
			CancelledTemplate: "\t",
		},
	)
	require.ErrorContains(t, err, "validate order events worker config")
	require.ErrorContains(t, err, "default channel must be non-empty")
}

func TestOrderEventsWorkerTickUsesMetadataCorrelationIDWhenPresent(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()

	called := false

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: newOrderConfirmedEventWithCorrelation(eventID.String(), uuid.NewString(), "user-1", "corr-from-metadata"),
			}}, nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, input notification.HandleOrderEventInput) error {
				called = true
				require.Equal(t, "corr-from-metadata", input.CorrelationID)
				return nil
			},
		},
		orderEventsPublisherStub{},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, called)
}

func TestOrderEventsWorkerTickUsesEnvelopeCorrelationIDWhenMetadataBlank(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()

	called := false

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Metadata: commonkafka.EventMetadata{CorrelationID: "corr-from-envelope"}},
				Message:  newOrderConfirmedEvent(eventID.String(), uuid.NewString(), "user-1"),
			}}, nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, input notification.HandleOrderEventInput) error {
				called = true
				require.Equal(t, "corr-from-envelope", input.CorrelationID)
				return nil
			},
		},
		orderEventsPublisherStub{},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, called)
}

func TestOrderEventsWorkerTickHandlesOrderCancelled(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()
	called := false
	commitCalls := 0

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: newOrderCancelledEvent(eventID.String(), uuid.NewString(), "user-2", "stock_unavailable", "stock unavailable"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, input notification.HandleOrderEventInput) error {
				called = true
				require.Equal(t, "order.cancelled", input.SourceEventName)
				require.Equal(t, "order-cancelled", input.TemplateKey)
				require.Contains(t, input.Body, "cancelled")
				return nil
			},
		},
		orderEventsPublisherStub{},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, called)
	require.Equal(t, 1, commitCalls)
}

func TestOrderEventsWorkerTickIdempotentReplaySkipsProcessedRequest(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()

	called := false
	commitCalls := 0

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: newOrderConfirmedEvent(eventID.String(), uuid.NewString(), "user-3"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, _ notification.HandleOrderEventInput) error {
				called = true
				return nil
			},
		},
		orderEventsPublisherStub{},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, called)
	require.Equal(t, 1, commitCalls)
}

func TestOrderEventsWorkerTickContinuesAfterMalformedPayload(t *testing.T) {
	t.Parallel()

	validEventID := uuid.New()
	handledCount := 0
	commitCalls := 0

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{
				{Envelope: commonkafka.EventEnvelope{Topic: "order.events"}, Message: newOrderConfirmedEvent("not-uuid", uuid.NewString(), "user-1")},
				{Envelope: commonkafka.EventEnvelope{Topic: "order.events"}, Message: newOrderConfirmedEvent(validEventID.String(), uuid.NewString(), "user-1")},
			}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, input notification.HandleOrderEventInput) error {
				handledCount++
				require.Equal(t, validEventID, input.EventID)
				return nil
			},
		},
		orderEventsPublisherStub{},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, handledCount)
	require.Equal(t, 1, commitCalls)
}

func TestOrderEventsWorkerTickRepublishesRetryAndCommitsOnServiceError(t *testing.T) {
	t.Parallel()

	firstEventID := uuid.New()
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{
				{Envelope: commonkafka.EventEnvelope{Topic: "order.events", Key: []byte("order-key"), Payload: []byte("payload-body")}, Message: newOrderConfirmedEvent(firstEventID.String(), uuid.NewString(), "user-1")},
			}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, input notification.HandleOrderEventInput) error {
				if input.EventID == firstEventID {
					return errors.New("service boom")
				}
				return nil
			},
		},
		orderEventsPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "order.events.retry", published[0].Topic)
	assertRetryHeaders(t, published[0].Headers, retryHeaderExpectations{
		Attempt:       "1",
		MaxAttempts:   "3",
		OriginalTopic: "order.events",
		ErrorCode:     "NOTIFICATION_HANDLE_ORDER_EVENT_FAILED",
		ErrorMessage:  "service boom",
		ConsumerGroup: "notification-svc-order-events-v1",
	})
	require.Equal(t, "1", published[0].Headers[commonkafka.HeaderRetryAttempt])
	require.Equal(t, []byte("order-key"), published[0].Key)
	require.Equal(t, []byte("payload-body"), published[0].Payload)
}

func TestOrderEventsWorkerTickPollErrorIsRecoverable(t *testing.T) {
	t.Parallel()

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return nil, errors.New("poll boom")
		}},
		notificationServiceStub{},
		orderEventsPublisherStub{},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
}

func TestOrderEventsWorkerTickCommitErrorReturnsError(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: newOrderConfirmedEvent(eventID.String(), uuid.NewString(), "user-1"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			return errors.New("commit boom")
		}},
		notificationServiceStub{},
		orderEventsPublisherStub{},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.ErrorContains(t, worker.Tick(context.Background()), "commit order event offsets")
}

func TestOrderEventsWorkerTickRoutesNonRetriableToDLQAndCommits(t *testing.T) {
	t.Parallel()

	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "order.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message:  newOrderConfirmedEvent("not-uuid", uuid.NewString(), "user-1"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{},
		orderEventsPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "order.events.dlq", published[0].Topic)
	assertRetryHeaders(t, published[0].Headers, retryHeaderExpectations{
		Attempt:       "0",
		MaxAttempts:   "3",
		OriginalTopic: "order.events",
		ErrorCode:     "NOTIFICATION_INVALID_EVENT_PAYLOAD",
		ErrorMessage:  "parse order confirmed: parse event id: invalid UUID length: 8",
		ConsumerGroup: "notification-svc-order-events-v1",
	})
	require.Equal(t, commonkafka.DLQReasonNonRetryable, published[0].Headers[commonkafka.HeaderDLQReason])
	assertHeaderRFC3339(t, published[0].Headers, commonkafka.HeaderDLQAt)
}

func TestOrderEventsWorkerTickServiceNonRetriableErrorRoutesDLQ(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "order.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message:  newOrderConfirmedEvent(eventID.String(), uuid.NewString(), "user-1"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{handleOrderEventFunc: func(context.Context, notification.HandleOrderEventInput) error {
			return commonkafka.ClassifyError(errors.New("policy validation failed"))
		}},
		orderEventsPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "order.events.dlq", published[0].Topic)
	assertRetryHeaders(t, published[0].Headers, retryHeaderExpectations{
		Attempt:       "0",
		MaxAttempts:   "3",
		OriginalTopic: "order.events",
		ErrorCode:     "NOTIFICATION_HANDLE_ORDER_EVENT_FAILED",
		ErrorMessage:  "policy validation failed",
		ConsumerGroup: "notification-svc-order-events-v1",
	})
	require.Equal(t, commonkafka.DLQReasonNonRetryable, published[0].Headers[commonkafka.HeaderDLQReason])
	assertHeaderRFC3339(t, published[0].Headers, commonkafka.HeaderDLQAt)
}

func TestOrderEventsWorkerTickRetriableAtMaxRoutesDLQAndCommits(t *testing.T) {
	t.Parallel()

	firstFailedAt := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{
					Topic:   "order.events.retry",
					Key:     []byte("order-key"),
					Payload: []byte("payload-body"),
					Headers: map[string]string{
						commonkafka.HeaderRetryAttempt:       "3",
						commonkafka.HeaderRetryMaxAttempts:   "3",
						commonkafka.HeaderRetryOriginalTopic: "order.events",
						commonkafka.HeaderRetryFirstFailedAt: firstFailedAt.Format(time.RFC3339),
					},
				},
				Message: newOrderConfirmedEvent(eventID.String(), uuid.NewString(), "user-1"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{handleOrderEventFunc: func(context.Context, notification.HandleOrderEventInput) error {
			return errors.New("service boom")
		}},
		orderEventsPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "order.events.dlq", published[0].Topic)
	assertRetryHeaders(t, published[0].Headers, retryHeaderExpectations{
		Attempt:       "3",
		MaxAttempts:   "3",
		OriginalTopic: "order.events",
		FirstFailedAt: firstFailedAt,
		ErrorCode:     "NOTIFICATION_HANDLE_ORDER_EVENT_FAILED",
		ErrorMessage:  "service boom",
		ConsumerGroup: "notification-svc-order-events-v1",
	})
	require.Equal(t, commonkafka.DLQReasonMaxAttemptsExceeded, published[0].Headers[commonkafka.HeaderDLQReason])
	assertHeaderRFC3339(t, published[0].Headers, commonkafka.HeaderDLQAt)
}

func TestOrderEventsWorkerTickUnsupportedMessageRoutesDLQ(t *testing.T) {
	t.Parallel()

	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)
	serviceCalled := false

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "order.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message:  &commonv1.EventMetadata{},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{handleOrderEventFunc: func(context.Context, notification.HandleOrderEventInput) error {
			serviceCalled = true
			return nil
		}},
		orderEventsPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.False(t, serviceCalled)
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "order.events.dlq", published[0].Topic)
	assertRetryHeaders(t, published[0].Headers, retryHeaderExpectations{
		Attempt:       "0",
		MaxAttempts:   "3",
		OriginalTopic: "order.events",
		ErrorCode:     "NOTIFICATION_INVALID_EVENT_PAYLOAD",
		ErrorMessage:  "unsupported order event message: *commonv1.EventMetadata",
		ConsumerGroup: "notification-svc-order-events-v1",
	})
	require.Equal(t, commonkafka.DLQReasonNonRetryable, published[0].Headers[commonkafka.HeaderDLQReason])
	assertHeaderRFC3339(t, published[0].Headers, commonkafka.HeaderDLQAt)
}

func TestOrderEventsWorkerTickRepublishFailureSkipsCommit(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()
	commitCalls := 0

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "order.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message:  newOrderConfirmedEvent(eventID.String(), uuid.NewString(), "user-1"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{handleOrderEventFunc: func(context.Context, notification.HandleOrderEventInput) error {
			return errors.New("service boom")
		}},
		orderEventsPublisherStub{publishFunc: func(context.Context, commonkafka.EventEnvelope) error {
			return errors.New("publish boom")
		}},
		validOrderEventsWorkerConfig(),
	)
	require.NoError(t, err)

	err = worker.Tick(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "republish order event")
	require.Equal(t, 0, commitCalls)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func validOrderEventsWorkerConfig() OrderEventsWorkerConfig {
	return OrderEventsWorkerConfig{
		PollInterval:      time.Millisecond,
		ConsumerGroupName: "notification-svc-order-events-v1",
		MaxRetryAttempts:  3,
		DefaultChannel:    "in_app",
		ConfirmedTemplate: "order %s confirmed",
		CancelledTemplate: "order %s cancelled: %s",
	}
}

func newOrderConfirmedEvent(eventID string, orderID string, userID string) *orderv1.OrderConfirmed {
	return &orderv1.OrderConfirmed{
		Metadata: &commonv1.EventMetadata{
			EventId:    eventID,
			EventName:  "order.confirmed",
			Producer:   "order-svc",
			OccurredAt: timestamppb.Now(),
		},
		OrderId: orderID,
		UserId:  userID,
	}
}

func newOrderConfirmedEventWithCorrelation(eventID string, orderID string, userID string, correlationID string) *orderv1.OrderConfirmed {
	payload := newOrderConfirmedEvent(eventID, orderID, userID)
	payload.Metadata.CorrelationId = correlationID

	return payload
}

func newOrderCancelledEvent(eventID string, orderID string, userID string, reasonCode string, reasonMessage string) *orderv1.OrderCancelled {
	return &orderv1.OrderCancelled{
		Metadata: &commonv1.EventMetadata{
			EventId:    eventID,
			EventName:  "order.cancelled",
			Producer:   "order-svc",
			OccurredAt: timestamppb.Now(),
		},
		OrderId:             orderID,
		UserId:              userID,
		CancelReasonCode:    reasonCode,
		CancelReasonMessage: reasonMessage,
	}
}

type retryHeaderExpectations struct {
	Attempt       string
	MaxAttempts   string
	OriginalTopic string
	FirstFailedAt time.Time
	LastFailedAt  time.Time
	ErrorCode     string
	ErrorMessage  string
	ConsumerGroup string
}

func assertRetryHeaders(t *testing.T, headers map[string]string, expected retryHeaderExpectations) {
	t.Helper()

	require.Equal(t, expected.Attempt, headers[commonkafka.HeaderRetryAttempt])
	require.Equal(t, expected.MaxAttempts, headers[commonkafka.HeaderRetryMaxAttempts])
	require.Equal(t, expected.OriginalTopic, headers[commonkafka.HeaderRetryOriginalTopic])
	if expected.FirstFailedAt.IsZero() {
		assertHeaderRFC3339(t, headers, commonkafka.HeaderRetryFirstFailedAt)
	} else {
		require.Equal(t, expected.FirstFailedAt.Format(time.RFC3339), headers[commonkafka.HeaderRetryFirstFailedAt])
	}

	if expected.LastFailedAt.IsZero() {
		assertHeaderRFC3339(t, headers, commonkafka.HeaderRetryLastFailedAt)
	} else {
		require.Equal(t, expected.LastFailedAt.Format(time.RFC3339), headers[commonkafka.HeaderRetryLastFailedAt])
	}

	require.Equal(t, expected.ErrorCode, headers[commonkafka.HeaderRetryErrorCode])
	require.Equal(t, expected.ErrorMessage, headers[commonkafka.HeaderRetryErrorMessage])
	require.Equal(t, expected.ConsumerGroup, headers[commonkafka.HeaderRetryConsumerGroup])
}

func assertHeaderRFC3339(t *testing.T, headers map[string]string, key string) {
	t.Helper()

	raw := headers[key]
	require.NotEmpty(t, raw)

	_, err := time.Parse(time.RFC3339, raw)
	require.NoError(t, err)
}

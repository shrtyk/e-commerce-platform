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

func (s notificationServiceStub) HandleOrderEvent(ctx context.Context, input notification.HandleOrderEventInput) error {
	if s.handleOrderEventFunc == nil {
		return nil
	}

	return s.handleOrderEventFunc(ctx, input)
}

func TestOrderEventsWorkerTickHandlesOrderConfirmed(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	calledRequest := false
	commitCalls := 0

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: newOrderConfirmedEvent(eventID.String(), uuid.NewString(), "user-1"),
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		notificationServiceStub{
			handleOrderEventFunc: func(_ context.Context, input notification.HandleOrderEventInput) error {
				calledRequest = true
				require.Equal(t, eventID, input.EventID)
				require.Equal(t, "notification-svc-order-events-v1", input.ConsumerGroupName)
				require.Equal(t, "order.confirmed", input.SourceEventName)
				require.Equal(t, "in_app", input.Channel)
				require.Equal(t, "user-1", input.Recipient)
				require.Equal(t, "order-confirmed", input.TemplateKey)
				require.Contains(t, input.Body, "confirmed")
				require.Equal(t, now, input.AttemptedAt)
				return nil
			},
		},
		OrderEventsWorkerConfig{PollInterval: time.Millisecond, ConsumerGroupName: "notification-svc-order-events-v1"},
	)
	require.NoError(t, err)
	worker.now = func() time.Time { return now }

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, calledRequest)
	require.Equal(t, 1, commitCalls)
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
		OrderEventsWorkerConfig{PollInterval: time.Millisecond, ConsumerGroupName: "notification-svc-order-events-v1"},
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
		OrderEventsWorkerConfig{PollInterval: time.Millisecond, ConsumerGroupName: "notification-svc-order-events-v1"},
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
				{Message: newOrderConfirmedEvent("not-uuid", uuid.NewString(), "user-1")},
				{Message: newOrderConfirmedEvent(validEventID.String(), uuid.NewString(), "user-1")},
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
		OrderEventsWorkerConfig{PollInterval: time.Millisecond, ConsumerGroupName: "notification-svc-order-events-v1"},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, handledCount)
	require.Equal(t, 1, commitCalls)
}

func TestOrderEventsWorkerTickReturnsErrorWithoutCommitOnServiceError(t *testing.T) {
	t.Parallel()

	firstEventID := uuid.New()
	secondEventID := uuid.New()
	commitCalls := 0

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{
				{Message: newOrderConfirmedEvent(firstEventID.String(), uuid.NewString(), "user-1")},
				{Message: newOrderConfirmedEvent(secondEventID.String(), uuid.NewString(), "user-2")},
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

				require.NotEqual(t, secondEventID, input.EventID)

				return nil
			},
		},
		OrderEventsWorkerConfig{PollInterval: time.Millisecond, ConsumerGroupName: "notification-svc-order-events-v1"},
	)
	require.NoError(t, err)

	require.ErrorContains(t, worker.Tick(context.Background()), "retryable order event handling")
	require.Equal(t, 0, commitCalls)
}

func TestOrderEventsWorkerTickPollErrorIsRecoverable(t *testing.T) {
	t.Parallel()

	worker, err := NewOrderEventsWorker(
		testLogger(),
		orderEventsConsumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return nil, errors.New("poll boom")
		}},
		notificationServiceStub{},
		OrderEventsWorkerConfig{PollInterval: time.Millisecond, ConsumerGroupName: "notification-svc-order-events-v1"},
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
		OrderEventsWorkerConfig{PollInterval: time.Millisecond, ConsumerGroupName: "notification-svc-order-events-v1"},
	)
	require.NoError(t, err)

	require.ErrorContains(t, worker.Tick(context.Background()), "commit order event offsets")
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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

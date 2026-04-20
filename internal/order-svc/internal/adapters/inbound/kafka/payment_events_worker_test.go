package kafka

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type consumerStub struct {
	pollFunc func(ctx context.Context) ([]commonkafka.ConsumedMessage, error)
}

func (s consumerStub) Poll(ctx context.Context) ([]commonkafka.ConsumedMessage, error) {
	if s.pollFunc == nil {
		return nil, nil
	}

	return s.pollFunc(ctx)
}

type paymentOutcomeHandlerStub struct {
	handleSucceededFunc func(ctx context.Context, input checkout.HandlePaymentSucceededInput) error
	handleFailedFunc    func(ctx context.Context, input checkout.HandlePaymentFailedInput) error
}

func (s paymentOutcomeHandlerStub) HandlePaymentSucceeded(ctx context.Context, input checkout.HandlePaymentSucceededInput) error {
	if s.handleSucceededFunc == nil {
		return nil
	}

	return s.handleSucceededFunc(ctx, input)
}

func (s paymentOutcomeHandlerStub) HandlePaymentFailed(ctx context.Context, input checkout.HandlePaymentFailedInput) error {
	if s.handleFailedFunc == nil {
		return nil
	}

	return s.handleFailedFunc(ctx, input)
}

func TestPaymentEventsWorkerTickDispatchesPaymentSucceeded(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	called := false

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: &paymentv1.PaymentSucceeded{OrderId: orderID.String(), ProcessedAt: timestamppb.Now()},
			}}, nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(_ context.Context, input checkout.HandlePaymentSucceededInput) error {
			called = true
			require.Equal(t, orderID, input.OrderID)
			return nil
		}},
		PaymentEventsWorkerConfig{PollInterval: 25},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, called)
}

func TestPaymentEventsWorkerTickDispatchesPaymentFailed(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	called := false

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: &paymentv1.PaymentFailed{OrderId: orderID.String(), FailureCode: "issuer_declined", ProcessedAt: timestamppb.Now()},
			}}, nil
		}},
		paymentOutcomeHandlerStub{handleFailedFunc: func(_ context.Context, input checkout.HandlePaymentFailedInput) error {
			called = true
			require.Equal(t, orderID, input.OrderID)
			require.Equal(t, "issuer_declined", input.FailureCode)
			return nil
		}},
		PaymentEventsWorkerConfig{PollInterval: 25},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, called)
}

func TestPaymentEventsWorkerTickIgnoresUnsupportedMessages(t *testing.T) {
	t.Parallel()

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Message: &paymentv1.PaymentInitiated{OrderId: uuid.NewString()},
			}}, nil
		}},
		paymentOutcomeHandlerStub{},
		PaymentEventsWorkerConfig{PollInterval: 25},
	)
	require.NoError(t, err)
	require.NoError(t, worker.Tick(context.Background()))
}

func TestPaymentEventsWorkerTickReturnsPollError(t *testing.T) {
	t.Parallel()

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return nil, errors.New("poll failed")
		}},
		paymentOutcomeHandlerStub{},
		PaymentEventsWorkerConfig{PollInterval: 25},
	)
	require.NoError(t, err)

	err = worker.Tick(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "poll payment events")
}

func TestPaymentEventsWorkerTickContinuesAfterMalformedMessage(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	handled := false

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{
				{Message: &paymentv1.PaymentFailed{OrderId: "not-uuid", FailureCode: "bad", ProcessedAt: timestamppb.Now()}},
				{Message: &paymentv1.PaymentSucceeded{OrderId: orderID.String(), ProcessedAt: timestamppb.Now()}},
			}, nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(_ context.Context, input checkout.HandlePaymentSucceededInput) error {
			handled = true
			require.Equal(t, orderID, input.OrderID)
			return nil
		}},
		PaymentEventsWorkerConfig{PollInterval: 25},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, handled)
}

func TestPaymentEventsWorkerTickContinuesAfterHandlerError(t *testing.T) {
	t.Parallel()

	firstOrderID := uuid.New()
	secondOrderID := uuid.New()
	calledSecond := false

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{
				{Message: &paymentv1.PaymentSucceeded{OrderId: firstOrderID.String(), ProcessedAt: timestamppb.Now()}},
				{Message: &paymentv1.PaymentSucceeded{OrderId: secondOrderID.String(), ProcessedAt: timestamppb.Now()}},
			}, nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(_ context.Context, input checkout.HandlePaymentSucceededInput) error {
			if input.OrderID == firstOrderID {
				return errors.New("boom")
			}

			if input.OrderID == secondOrderID {
				calledSecond = true
			}

			return nil
		}},
		PaymentEventsWorkerConfig{PollInterval: 25},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.True(t, calledSecond)
}

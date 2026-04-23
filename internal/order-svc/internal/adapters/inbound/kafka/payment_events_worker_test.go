package kafka

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type consumerStub struct {
	pollFunc   func(ctx context.Context) ([]commonkafka.ConsumedMessage, error)
	commitFunc func(ctx context.Context) error
}

func (s consumerStub) Poll(ctx context.Context) ([]commonkafka.ConsumedMessage, error) {
	if s.pollFunc == nil {
		return nil, nil
	}

	return s.pollFunc(ctx)
}

func (s consumerStub) CommitUncommittedOffsets(ctx context.Context) error {
	if s.commitFunc == nil {
		return nil
	}

	return s.commitFunc(ctx)
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

type eventPublisherStub struct {
	publishFunc func(ctx context.Context, envelope commonkafka.EventEnvelope) error
}

func (s eventPublisherStub) Publish(ctx context.Context, envelope commonkafka.EventEnvelope) error {
	if s.publishFunc == nil {
		return nil
	}

	return s.publishFunc(ctx, envelope)
}

type consumerIdempotencyRepositoryStub struct {
	createFunc func(ctx context.Context, input outbound.CreateConsumerIdempotencyInput) error
	existsFunc func(ctx context.Context, eventID uuid.UUID, consumerGroupName string) (bool, error)
}

func (s consumerIdempotencyRepositoryStub) Create(ctx context.Context, input outbound.CreateConsumerIdempotencyInput) error {
	if s.createFunc == nil {
		return nil
	}

	return s.createFunc(ctx, input)
}

func (s consumerIdempotencyRepositoryStub) Exists(ctx context.Context, eventID uuid.UUID, consumerGroupName string) (bool, error) {
	if s.existsFunc == nil {
		return false, nil
	}

	return s.existsFunc(ctx, eventID, consumerGroupName)
}

func TestPaymentEventsWorkerTickSuccessCommitsOffset(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	handlerCalls := 0
	commitCalls := 0

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "payment.events"},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(_ context.Context, input checkout.HandlePaymentSucceededInput) error {
			handlerCalls++
			require.Equal(t, orderID, input.OrderID)
			return nil
		}},
		eventPublisherStub{},
		consumerIdempotencyRepositoryStub{},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, handlerCalls)
	require.Equal(t, 1, commitCalls)
}

func TestPaymentEventsWorkerTickDuplicateReplaySkipsHandlerAndCommits(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()
	handlerCalls := 0
	commitCalls := 0

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "payment.events"},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     uuid.NewString(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			handlerCalls++
			return nil
		}},
		eventPublisherStub{},
		consumerIdempotencyRepositoryStub{existsFunc: func(context.Context, uuid.UUID, string) (bool, error) {
			return true, nil
		}},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 0, handlerCalls)
	require.Equal(t, 1, commitCalls)
}

func TestPaymentEventsWorkerTickRetriableErrorRepublishRetryThenCommit(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{
					Topic:   "payment.events",
					Key:     []byte("order-key"),
					Payload: []byte("payload-body"),
					Metadata: commonkafka.EventMetadata{
						EventID:       eventID.String(),
						CorrelationID: "corr-1",
					},
				},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			return commonkafka.ClassifyError(kerr.LeaderNotAvailable)
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.retry", published[0].Topic)
	require.Equal(t, []byte("order-key"), published[0].Key)
	require.Equal(t, []byte("payload-body"), published[0].Payload)
	require.Equal(t, "1", published[0].Headers[commonkafka.HeaderRetryAttempt])
}

func TestPaymentEventsWorkerTickHandlerFailureDoesNotCreateIdempotencyMarker(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	handlerCalls := 0
	createCalls := 0
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{
					Topic:   "payment.events",
					Key:     []byte("order-key"),
					Payload: []byte("payload-body"),
				},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			handlerCalls++
			return commonkafka.ClassifyError(kerr.LeaderNotAvailable)
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{createFunc: func(context.Context, outbound.CreateConsumerIdempotencyInput) error {
			createCalls++
			return nil
		}},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, handlerCalls)
	require.Equal(t, 0, createCalls)
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.retry", published[0].Topic)
}

func TestPaymentEventsWorkerTickIdempotencyExistsErrorRepublishesRetry(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	handlerCalls := 0
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "payment.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			handlerCalls++
			return nil
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{existsFunc: func(context.Context, uuid.UUID, string) (bool, error) {
			return false, errors.New("db timeout")
		}},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 0, handlerCalls)
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.retry", published[0].Topic)
}

func TestPaymentEventsWorkerTickIdempotencyInvalidArgRepublishesDLQ(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	handlerCalls := 0
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "payment.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			handlerCalls++
			return nil
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{existsFunc: func(context.Context, uuid.UUID, string) (bool, error) {
			return false, outbound.ErrInvalidConsumerIdempotencyArg
		}},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 0, handlerCalls)
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.dlq", published[0].Topic)
	require.Equal(t, commonkafka.DLQReasonNonRetryable, published[0].Headers[commonkafka.HeaderDLQReason])
}

func TestPaymentEventsWorkerTickRedeliveryBeforeSuccessStillInvokesHandler(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	handlerCalls := 0
	createCalls := 0
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)
	pollCalls := 0

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			pollCalls++

			retryHeaders := map[string]string{}
			if pollCalls == 2 {
				retryHeaders[commonkafka.HeaderRetryAttempt] = "1"
				retryHeaders[commonkafka.HeaderRetryMaxAttempts] = "3"
				retryHeaders[commonkafka.HeaderRetryOriginalTopic] = "payment.events"
			}

			topic := "payment.events"
			if pollCalls == 2 {
				topic = "payment.events.retry"
			}

			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{
					Topic:   topic,
					Key:     []byte("order-key"),
					Payload: []byte("payload-body"),
					Headers: retryHeaders,
				},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			handlerCalls++
			if handlerCalls == 1 {
				return commonkafka.ClassifyError(kerr.LeaderNotAvailable)
			}

			return nil
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{createFunc: func(context.Context, outbound.CreateConsumerIdempotencyInput) error {
			createCalls++
			return nil
		}},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 2, handlerCalls)
	require.Equal(t, 1, createCalls)
	require.Equal(t, 2, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.retry", published[0].Topic)
}

func TestPaymentEventsWorkerTickRetriableAtMaxRepublishDLQThenCommit(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{
					Topic:   "payment.events.retry",
					Key:     []byte("order-key"),
					Payload: []byte("payload-body"),
					Headers: map[string]string{
						commonkafka.HeaderRetryAttempt:       "3",
						commonkafka.HeaderRetryMaxAttempts:   "3",
						commonkafka.HeaderRetryOriginalTopic: "payment.events",
					},
					Metadata: commonkafka.EventMetadata{EventID: eventID.String()},
				},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			return commonkafka.ClassifyError(kerr.LeaderNotAvailable)
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.dlq", published[0].Topic)
	require.Equal(t, commonkafka.DLQReasonMaxAttemptsExceeded, published[0].Headers[commonkafka.HeaderDLQReason])
}

func TestPaymentEventsWorkerTickIdempotencyCreateErrorRepublishRetryThenCommit(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "payment.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message: &paymentv1.PaymentFailed{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					FailureCode: "issuer_declined",
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleFailedFunc: func(_ context.Context, input checkout.HandlePaymentFailedInput) error {
			require.Equal(t, orderID, input.OrderID)
			return nil
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{createFunc: func(context.Context, outbound.CreateConsumerIdempotencyInput) error {
			return errors.New("idempotency boom")
		}},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.retry", published[0].Topic)
}

func TestPaymentEventsWorkerTickRepublishFailureSkipsCommit(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	commitCalls := 0

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{Topic: "payment.events", Key: []byte("order-key"), Payload: []byte("payload-body")},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			return commonkafka.ClassifyError(kerr.LeaderNotAvailable)
		}},
		eventPublisherStub{publishFunc: func(context.Context, commonkafka.EventEnvelope) error {
			return errors.New("publish boom")
		}},
		consumerIdempotencyRepositoryStub{},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	err = worker.Tick(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "republish payment event")
	require.Equal(t, 0, commitCalls)
}

func TestPaymentEventsWorkerTickMalformedRetryHeadersDeterministic(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	eventID := uuid.New()
	commitCalls := 0
	published := make([]commonkafka.EventEnvelope, 0, 1)

	worker, err := NewPaymentEventsWorker(
		consumerStub{pollFunc: func(context.Context) ([]commonkafka.ConsumedMessage, error) {
			return []commonkafka.ConsumedMessage{{
				Envelope: commonkafka.EventEnvelope{
					Topic:   "payment.events.retry",
					Key:     []byte("order-key"),
					Payload: []byte("payload-body"),
					Headers: map[string]string{
						commonkafka.HeaderRetryAttempt:       "bad",
						commonkafka.HeaderRetryMaxAttempts:   "bad",
						commonkafka.HeaderRetryOriginalTopic: "payment.events",
					},
					Metadata: commonkafka.EventMetadata{EventID: eventID.String()},
				},
				Message: &paymentv1.PaymentSucceeded{
					Metadata:    &commonv1.EventMetadata{EventId: eventID.String()},
					OrderId:     orderID.String(),
					ProcessedAt: timestamppb.Now(),
				},
			}}, nil
		}, commitFunc: func(context.Context) error {
			commitCalls++
			return nil
		}},
		paymentOutcomeHandlerStub{handleSucceededFunc: func(context.Context, checkout.HandlePaymentSucceededInput) error {
			return commonkafka.ClassifyError(kerr.LeaderNotAvailable)
		}},
		eventPublisherStub{publishFunc: func(_ context.Context, envelope commonkafka.EventEnvelope) error {
			published = append(published, envelope)
			return nil
		}},
		consumerIdempotencyRepositoryStub{},
		PaymentEventsWorkerConfig{PollInterval: 25, ConsumerGroupName: "order-svc-payment-events-v1", MaxRetryAttempts: 3},
	)
	require.NoError(t, err)

	require.NoError(t, worker.Tick(context.Background()))
	require.Equal(t, 1, commitCalls)
	require.Len(t, published, 1)
	require.Equal(t, "payment.events.retry", published[0].Topic)
	require.Equal(t, "1", published[0].Headers[commonkafka.HeaderRetryAttempt])
}

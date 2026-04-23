package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

const (
	errorCodeHandlePaymentSucceeded = "ORDER_HANDLE_PAYMENT_SUCCEEDED_FAILED"
	errorCodeHandlePaymentFailed    = "ORDER_HANDLE_PAYMENT_FAILED"
	errorCodeInvalidPayload         = "ORDER_INVALID_EVENT_PAYLOAD"
	errorCodeIdempotency            = "ORDER_CONSUMER_IDEMPOTENCY_FAILED"
)

type paymentEventsConsumer interface {
	Poll(ctx context.Context) ([]commonkafka.ConsumedMessage, error)
	CommitUncommittedOffsets(ctx context.Context) error
}

type paymentOutcomeHandler interface {
	HandlePaymentSucceeded(ctx context.Context, input checkout.HandlePaymentSucceededInput) error
	HandlePaymentFailed(ctx context.Context, input checkout.HandlePaymentFailedInput) error
}

type eventPublisher interface {
	Publish(ctx context.Context, envelope commonkafka.EventEnvelope) error
}

type PaymentEventsWorkerConfig struct {
	PollInterval       time.Duration
	ConsumerGroupName  string
	MaxRetryAttempts   int
	ConsumerDomainName string
}

func (c PaymentEventsWorkerConfig) Validate() error {
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll interval must be positive")
	}

	if strings.TrimSpace(c.ConsumerGroupName) == "" {
		return fmt.Errorf("consumer group name must be non-empty")
	}

	if c.MaxRetryAttempts < 1 {
		return fmt.Errorf("max retry attempts must be >= 1")
	}

	return nil
}

type PaymentEventsWorker struct {
	consumer              paymentEventsConsumer
	handler               paymentOutcomeHandler
	publisher             eventPublisher
	consumerIdempotencies outbound.ConsumerIdempotencyRepository
	router                *commonkafka.ReliabilityRouter
	config                PaymentEventsWorkerConfig
	ticker                func(time.Duration) ticker
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type stdTicker struct {
	inner *time.Ticker
}

func (t stdTicker) C() <-chan time.Time {
	return t.inner.C
}

func (t stdTicker) Stop() {
	t.inner.Stop()
}

func NewPaymentEventsWorker(
	consumer paymentEventsConsumer,
	handler paymentOutcomeHandler,
	publisher eventPublisher,
	consumerIdempotencies outbound.ConsumerIdempotencyRepository,
	cfg PaymentEventsWorkerConfig,
) (*PaymentEventsWorker, error) {
	if consumer == nil {
		return nil, fmt.Errorf("payment events consumer is nil")
	}

	if handler == nil {
		return nil, fmt.Errorf("payment outcome handler is nil")
	}

	if publisher == nil {
		return nil, fmt.Errorf("payment events publisher is nil")
	}

	if consumerIdempotencies == nil {
		return nil, fmt.Errorf("consumer idempotency repository is nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate payment events worker config: %w", err)
	}

	router, err := commonkafka.NewReliabilityRouter(cfg.ConsumerGroupName, cfg.MaxRetryAttempts)
	if err != nil {
		return nil, fmt.Errorf("create reliability router: %w", err)
	}

	return &PaymentEventsWorker{
		consumer:              consumer,
		handler:               handler,
		publisher:             publisher,
		consumerIdempotencies: consumerIdempotencies,
		router:                router,
		config:                cfg,
		ticker: func(d time.Duration) ticker {
			return stdTicker{inner: time.NewTicker(d)}
		},
	}, nil
}

func (w *PaymentEventsWorker) Run(ctx context.Context) error {
	if err := w.Tick(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}

		return err
	}

	t := w.ticker(w.config.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C():
			if err := w.Tick(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}

				return err
			}
		}
	}
}

func (w *PaymentEventsWorker) Tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return nil
	}

	messages, err := w.consumer.Poll(ctx)
	if err != nil {
		return fmt.Errorf("poll payment events: %w", err)
	}

	if len(messages) == 0 {
		return nil
	}

	for _, message := range messages {
		if err := w.handleMessage(ctx, message); err != nil {
			return err
		}
	}

	if err := w.consumer.CommitUncommittedOffsets(ctx); err != nil {
		return fmt.Errorf("commit payment event offsets: %w", err)
	}

	return nil
}

func (w *PaymentEventsWorker) handleMessage(ctx context.Context, consumed commonkafka.ConsumedMessage) error {
	eventID, err := eventIDFromMessage(consumed.Message)
	if err != nil {
		return w.routeAndRepublishFailure(ctx, consumed, commonkafka.FailureNonRetriable, errorCodeInvalidPayload, err.Error())
	}

	idempotencyExists, err := w.consumerIdempotencies.Exists(ctx, eventID, w.config.ConsumerGroupName)
	if err != nil {
		return w.routeAndRepublishFailure(
			ctx,
			consumed,
			classifyIdempotencyRepoError(err),
			errorCodeIdempotency,
			fmt.Sprintf("check idempotency marker: %v", err),
		)
	}

	if idempotencyExists {
		return nil
	}

	err = w.dispatchMessage(ctx, consumed.Message)
	if err != nil {
		code := errorCodeHandlePaymentSucceeded
		if _, ok := consumed.Message.(*paymentv1.PaymentFailed); ok {
			code = errorCodeHandlePaymentFailed
		}

		classification := commonkafka.FailureNonRetriable
		if commonkafka.IsRetriable(err) {
			classification = commonkafka.FailureRetriable
		}

		return w.routeAndRepublishFailure(ctx, consumed, classification, code, err.Error())
	}

	err = w.consumerIdempotencies.Create(ctx, outbound.CreateConsumerIdempotencyInput{
		EventID:           eventID,
		ConsumerGroupName: w.config.ConsumerGroupName,
	})
	if err != nil {
		if errors.Is(err, outbound.ErrConsumerIdempotencyDuplicate) {
			return nil
		}

		return w.routeAndRepublishFailure(
			ctx,
			consumed,
			classifyIdempotencyRepoError(err),
			errorCodeIdempotency,
			fmt.Sprintf("create idempotency marker: %v", err),
		)
	}

	return nil
}

func classifyIdempotencyRepoError(err error) commonkafka.FailureClassification {
	if errors.Is(err, outbound.ErrInvalidConsumerIdempotencyArg) {
		return commonkafka.FailureNonRetriable
	}

	return commonkafka.FailureRetriable
}

func (w *PaymentEventsWorker) dispatchMessage(ctx context.Context, message any) error {
	switch payload := message.(type) {
	case *paymentv1.PaymentSucceeded:
		orderID, err := uuid.Parse(strings.TrimSpace(payload.GetOrderId()))
		if err != nil {
			return fmt.Errorf("parse payment succeeded order id: %w", err)
		}

		if err := w.handler.HandlePaymentSucceeded(ctx, checkout.HandlePaymentSucceededInput{OrderID: orderID}); err != nil {
			return fmt.Errorf("handle payment succeeded: %w", err)
		}
	case *paymentv1.PaymentFailed:
		orderID, err := uuid.Parse(strings.TrimSpace(payload.GetOrderId()))
		if err != nil {
			return fmt.Errorf("parse payment failed order id: %w", err)
		}

		if err := w.handler.HandlePaymentFailed(ctx, checkout.HandlePaymentFailedInput{
			OrderID:     orderID,
			FailureCode: strings.TrimSpace(payload.GetFailureCode()),
		}); err != nil {
			return fmt.Errorf("handle payment failed: %w", err)
		}
	default:
		// treat unsupported payload as non-retriable malformed message
		return fmt.Errorf("unsupported payment event message: %T", payload)
	}

	return nil
}

func (w *PaymentEventsWorker) routeAndRepublishFailure(
	ctx context.Context,
	consumed commonkafka.ConsumedMessage,
	classification commonkafka.FailureClassification,
	errorCode string,
	errorMessage string,
) error {
	decision, err := w.router.RouteFailure(consumed.Envelope, classification, errorCode, compactErrorMessage(errorMessage))
	if err != nil {
		return fmt.Errorf("route payment event failure: %w", err)
	}

	if decision.Target == commonkafka.RoutingTargetNone {
		return nil
	}

	if err := w.publisher.Publish(ctx, decision.Envelope); err != nil {
		return fmt.Errorf("republish payment event to %s: %w", decision.Envelope.Topic, err)
	}

	return nil
}

func compactErrorMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "unknown"
	}

	if len(trimmed) <= 512 {
		return trimmed
	}

	return trimmed[:512]
}

func eventIDFromMessage(message any) (uuid.UUID, error) {
	var raw string

	switch payload := message.(type) {
	case *paymentv1.PaymentSucceeded:
		raw = payload.GetMetadata().GetEventId()
	case *paymentv1.PaymentFailed:
		raw = payload.GetMetadata().GetEventId()
	default:
		return uuid.Nil, fmt.Errorf("unsupported payment event message: %T", payload)
	}

	eventID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse event id: %w", err)
	}

	return eventID, nil
}

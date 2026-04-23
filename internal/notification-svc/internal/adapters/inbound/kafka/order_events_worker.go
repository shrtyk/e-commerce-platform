package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/service/notification"
)

const (
	defaultChannel        = "in_app"
	confirmedTemplateKey  = "order-confirmed"
	cancelledTemplateKey  = "order-cancelled"
	confirmedBodyTemplate = "order %s confirmed"
	cancelledBodyTemplate = "order %s cancelled: %s"

	errorCodeHandleOrderEvent = "NOTIFICATION_HANDLE_ORDER_EVENT_FAILED"
	errorCodeInvalidPayload   = "NOTIFICATION_INVALID_EVENT_PAYLOAD"
)

type orderEventsConsumer interface {
	Poll(ctx context.Context) ([]commonkafka.ConsumedMessage, error)
	CommitUncommittedOffsets(ctx context.Context) error
}

type orderEventsNotificationService interface {
	HandleOrderEvent(ctx context.Context, input notification.HandleOrderEventInput) error
}

type eventPublisher interface {
	Publish(ctx context.Context, envelope commonkafka.EventEnvelope) error
}

type OrderEventsWorkerConfig struct {
	PollInterval      time.Duration
	ConsumerGroupName string
	MaxRetryAttempts  int
}

func (c OrderEventsWorkerConfig) Validate() error {
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

type OrderEventsWorker struct {
	consumer            orderEventsConsumer
	notificationService orderEventsNotificationService
	publisher           eventPublisher
	router              *commonkafka.ReliabilityRouter
	logger              *slog.Logger
	config              OrderEventsWorkerConfig
	ticker              func(time.Duration) ticker
	now                 func() time.Time
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

func NewOrderEventsWorker(
	logger *slog.Logger,
	consumer orderEventsConsumer,
	notificationService orderEventsNotificationService,
	publisher eventPublisher,
	cfg OrderEventsWorkerConfig,
) (*OrderEventsWorker, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is nil")
	}

	if consumer == nil {
		return nil, fmt.Errorf("order events consumer is nil")
	}

	if notificationService == nil {
		return nil, fmt.Errorf("notification service is nil")
	}

	if publisher == nil {
		return nil, fmt.Errorf("order events publisher is nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate order events worker config: %w", err)
	}

	router, err := commonkafka.NewReliabilityRouter(cfg.ConsumerGroupName, cfg.MaxRetryAttempts)
	if err != nil {
		return nil, fmt.Errorf("create reliability router: %w", err)
	}

	return &OrderEventsWorker{
		consumer:            consumer,
		notificationService: notificationService,
		publisher:           publisher,
		router:              router,
		logger:              logger,
		config:              cfg,
		ticker: func(d time.Duration) ticker {
			return stdTicker{inner: time.NewTicker(d)}
		},
		now: time.Now,
	}, nil
}

func (w *OrderEventsWorker) Run(ctx context.Context) error {
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

func (w *OrderEventsWorker) Tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return nil
	}

	messages, err := w.consumer.Poll(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "order events worker poll failed", slog.Any("error", err))
		return nil
	}

	for _, message := range messages {
		if err := w.handleMessage(ctx, message); err != nil {
			return err
		}
	}

	if len(messages) == 0 {
		return nil
	}

	if err := w.consumer.CommitUncommittedOffsets(ctx); err != nil {
		return fmt.Errorf("commit order event offsets: %w", err)
	}

	return nil
}

func (w *OrderEventsWorker) handleMessage(ctx context.Context, consumed commonkafka.ConsumedMessage) error {
	switch payload := consumed.Message.(type) {
	case *orderv1.OrderConfirmed:
		return w.handleOrderConfirmed(ctx, consumed, payload)
	case *orderv1.OrderCancelled:
		return w.handleOrderCancelled(ctx, consumed, payload)
	default:
		return w.routeAndRepublishFailure(
			ctx,
			consumed,
			commonkafka.FailureNonRetriable,
			errorCodeInvalidPayload,
			fmt.Sprintf("unsupported order event message: %T", payload),
		)
	}
}

func (w *OrderEventsWorker) handleOrderConfirmed(ctx context.Context, consumed commonkafka.ConsumedMessage, payload *orderv1.OrderConfirmed) error {
	eventID, orderID, userID, err := parseOrderEvent(payload.GetMetadata().GetEventId(), payload.GetOrderId(), payload.GetUserId())
	if err != nil {
		return w.routeAndRepublishFailure(
			ctx,
			consumed,
			commonkafka.FailureNonRetriable,
			errorCodeInvalidPayload,
			fmt.Sprintf("parse order confirmed: %v", err),
		)
	}

	correlationID := correlationIDFromConsumedEvent(consumed, payload.GetMetadata(), eventID)

	return w.handleOrderEvent(ctx, handleOrderEventInput{
		consumed:      consumed,
		eventID:       eventID,
		sourceEventID: orderID,
		correlationID: correlationID,
		sourceEvent:   "order.confirmed",
		recipient:     userID,
		templateKey:   confirmedTemplateKey,
		body:          fmt.Sprintf(confirmedBodyTemplate, orderID.String()),
	})
}

func (w *OrderEventsWorker) handleOrderCancelled(ctx context.Context, consumed commonkafka.ConsumedMessage, payload *orderv1.OrderCancelled) error {
	eventID, orderID, userID, err := parseOrderEvent(payload.GetMetadata().GetEventId(), payload.GetOrderId(), payload.GetUserId())
	if err != nil {
		return w.routeAndRepublishFailure(
			ctx,
			consumed,
			commonkafka.FailureNonRetriable,
			errorCodeInvalidPayload,
			fmt.Sprintf("parse order cancelled: %v", err),
		)
	}

	correlationID := correlationIDFromConsumedEvent(consumed, payload.GetMetadata(), eventID)

	reason := strings.TrimSpace(payload.GetCancelReasonMessage())
	if reason == "" {
		reason = strings.TrimSpace(payload.GetCancelReasonCode())
	}
	if reason == "" {
		reason = "unspecified"
	}

	return w.handleOrderEvent(ctx, handleOrderEventInput{
		consumed:      consumed,
		eventID:       eventID,
		sourceEventID: orderID,
		correlationID: correlationID,
		sourceEvent:   "order.cancelled",
		recipient:     userID,
		templateKey:   cancelledTemplateKey,
		body:          fmt.Sprintf(cancelledBodyTemplate, orderID.String(), reason),
	})
}

type handleOrderEventInput struct {
	consumed      commonkafka.ConsumedMessage
	eventID       uuid.UUID
	sourceEventID uuid.UUID
	correlationID string
	sourceEvent   string
	recipient     string
	templateKey   string
	body          string
}

func (w *OrderEventsWorker) handleOrderEvent(ctx context.Context, input handleOrderEventInput) error {
	err := w.notificationService.HandleOrderEvent(ctx, notification.HandleOrderEventInput{
		EventID:           input.eventID,
		ConsumerGroupName: w.config.ConsumerGroupName,
		SourceEventID:     input.sourceEventID,
		CorrelationID:     input.correlationID,
		SourceEventName:   input.sourceEvent,
		Channel:           defaultChannel,
		Recipient:         input.recipient,
		TemplateKey:       input.templateKey,
		Body:              input.body,
		AttemptedAt:       w.now().UTC(),
	})
	if err != nil {
		classification := commonkafka.FailureRetriable
		if commonkafka.IsNonRetriable(err) {
			classification = commonkafka.FailureNonRetriable
		}

		return w.routeAndRepublishFailure(ctx, input.consumed, classification, errorCodeHandleOrderEvent, err.Error())
	}

	return nil
}

func (w *OrderEventsWorker) routeAndRepublishFailure(
	ctx context.Context,
	consumed commonkafka.ConsumedMessage,
	classification commonkafka.FailureClassification,
	errorCode string,
	errorMessage string,
) error {
	decision, err := w.router.RouteFailure(consumed.Envelope, classification, errorCode, compactErrorMessage(errorMessage))
	if err != nil {
		return fmt.Errorf("route order event failure: %w", err)
	}

	if decision.Target == commonkafka.RoutingTargetNone {
		return nil
	}

	if err := w.publisher.Publish(ctx, decision.Envelope); err != nil {
		return fmt.Errorf("republish order event to %s: %w", decision.Envelope.Topic, err)
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

func correlationIDFromConsumedEvent(consumed commonkafka.ConsumedMessage, metadata interface{ GetCorrelationId() string }, eventID uuid.UUID) string {
	if metadata != nil {
		if correlationID := strings.TrimSpace(metadata.GetCorrelationId()); correlationID != "" {
			return correlationID
		}
	}

	if correlationID := strings.TrimSpace(consumed.Envelope.Metadata.CorrelationID); correlationID != "" {
		return correlationID
	}

	return eventID.String()
}

func parseOrderEvent(eventIDRaw string, orderIDRaw string, userIDRaw string) (uuid.UUID, uuid.UUID, string, error) {
	eventID, err := uuid.Parse(strings.TrimSpace(eventIDRaw))
	if err != nil {
		return uuid.Nil, uuid.Nil, "", fmt.Errorf("parse event id: %w", err)
	}

	orderID, err := uuid.Parse(strings.TrimSpace(orderIDRaw))
	if err != nil {
		return uuid.Nil, uuid.Nil, "", fmt.Errorf("parse order id: %w", err)
	}

	userID := strings.TrimSpace(userIDRaw)
	if userID == "" {
		return uuid.Nil, uuid.Nil, "", fmt.Errorf("user id is empty")
	}

	return eventID, orderID, userID, nil
}

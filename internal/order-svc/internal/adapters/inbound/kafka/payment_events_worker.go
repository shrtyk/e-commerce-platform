package kafka

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

type paymentEventsConsumer interface {
	Poll(ctx context.Context) ([]commonkafka.ConsumedMessage, error)
}

type paymentOutcomeHandler interface {
	HandlePaymentSucceeded(ctx context.Context, input checkout.HandlePaymentSucceededInput) error
	HandlePaymentFailed(ctx context.Context, input checkout.HandlePaymentFailedInput) error
}

type PaymentEventsWorkerConfig struct {
	PollInterval time.Duration
}

func (c PaymentEventsWorkerConfig) Validate() error {
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll interval must be positive")
	}

	return nil
}

type PaymentEventsWorker struct {
	consumer paymentEventsConsumer
	handler  paymentOutcomeHandler
	config   PaymentEventsWorkerConfig
	ticker   func(time.Duration) ticker
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
	cfg PaymentEventsWorkerConfig,
) (*PaymentEventsWorker, error) {
	if consumer == nil {
		return nil, fmt.Errorf("payment events consumer is nil")
	}

	if handler == nil {
		return nil, fmt.Errorf("payment outcome handler is nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate payment events worker config: %w", err)
	}

	return &PaymentEventsWorker{
		consumer: consumer,
		handler:  handler,
		config:   cfg,
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

	for _, message := range messages {
		if err := w.handleMessage(ctx, message); err != nil {
			log.Printf("payment events worker: skip message: %v", err)
			continue
		}
	}

	return nil
}

func (w *PaymentEventsWorker) handleMessage(ctx context.Context, consumed commonkafka.ConsumedMessage) error {
	switch payload := consumed.Message.(type) {
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
		return nil
	}

	return nil
}

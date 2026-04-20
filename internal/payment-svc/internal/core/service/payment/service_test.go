package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/ports/outbound"
)

func TestServiceInitiatePayment(t *testing.T) {
	t.Run("returns invalid arg when repository missing", func(t *testing.T) {
		svc := NewService(nil, nil, nil, "payment-svc")

		_, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{})

		require.ErrorContains(t, err, "payment attempts repository is required")
	})

	t.Run("returns invalid arg when provider missing", func(t *testing.T) {
		svc := NewService(stubPaymentAttemptRepository{}, stubEventPublisher{}, nil, "payment-svc")

		_, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{})

		require.ErrorContains(t, err, "payment provider is required")
	})

	t.Run("returns invalid arg when publisher missing", func(t *testing.T) {
		svc := NewService(stubPaymentAttemptRepository{}, nil, stubPaymentProvider{}, "payment-svc")

		_, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{})

		require.ErrorContains(t, err, "event publisher is required")
	})

	t.Run("creates processing and marks succeeded", func(t *testing.T) {
		orderID := uuid.New()
		attemptID := uuid.New()
		processedAt := time.Now().UTC()
		initiated := domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusInitiated, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-1"}
		processing := domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusProcessing, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-1"}
		succeeded := domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusSucceeded, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-1", ProviderReference: "stub-ok", ProcessedAt: &processedAt}
		publisher := &capturingEventPublisher{}

		repo := stubPaymentAttemptRepository{
			createInitiatedFunc: func(_ context.Context, input outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				require.Equal(t, orderID, input.OrderID)
				return initiated, nil
			},
			markProcessingFunc: func(_ context.Context, paymentAttemptID uuid.UUID) (domain.PaymentAttempt, error) {
				require.Equal(t, attemptID, paymentAttemptID)
				return processing, nil
			},
			markSucceededFunc: func(_ context.Context, paymentAttemptID uuid.UUID, providerReference string) (domain.PaymentAttempt, error) {
				require.Equal(t, attemptID, paymentAttemptID)
				require.Equal(t, "stub-ok", providerReference)
				return succeeded, nil
			},
		}
		provider := stubPaymentProvider{chargeFunc: func(_ context.Context, input outbound.ChargePaymentInput) (outbound.ChargePaymentResult, error) {
			require.Equal(t, attemptID, input.PaymentAttemptID)
			return outbound.ChargePaymentResult{ProviderReference: "stub-ok"}, nil
		}}

		svc := NewService(repo, publisher, provider, "payment-svc")
		got, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: orderID, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-1"})

		require.NoError(t, err)
		require.Equal(t, succeeded, got.PaymentAttempt)
		require.Len(t, publisher.events, 2)
		require.Equal(t, "payment.initiated", publisher.events[0].EventName)
		require.Equal(t, "payment.succeeded", publisher.events[1].EventName)
	})

	t.Run("returns existing attempt for duplicate create when terminal", func(t *testing.T) {
		orderID := uuid.New()
		existing := domain.PaymentAttempt{PaymentAttemptID: uuid.New(), OrderID: orderID, Status: domain.PaymentStatusSucceeded}

		publisher := &capturingEventPublisher{}

		svc := NewService(stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{}, outbound.ErrPaymentAttemptDuplicate
			},
			getByOrderIDAndIdempotencyKeyFunc: func(context.Context, uuid.UUID, string) (domain.PaymentAttempt, error) {
				return existing, nil
			},
		}, publisher, stubPaymentProvider{}, "payment-svc")

		got, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: orderID, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-dup"})

		require.NoError(t, err)
		require.Equal(t, existing, got.PaymentAttempt)
		require.Empty(t, publisher.events)
	})

	t.Run("returns existing failed attempt for duplicate replay without reprocessing", func(t *testing.T) {
		orderID := uuid.New()
		existing := domain.PaymentAttempt{
			PaymentAttemptID: uuid.New(),
			OrderID:          orderID,
			Status:           domain.PaymentStatusFailed,
			Amount:           100,
			Currency:         "USD",
			ProviderName:     "stub",
			IdempotencyKey:   "id-failed-replay",
		}

		repo := stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{}, outbound.ErrPaymentAttemptDuplicate
			},
			getByOrderIDAndIdempotencyKeyFunc: func(context.Context, uuid.UUID, string) (domain.PaymentAttempt, error) {
				return existing, nil
			},
			markProcessingFunc: func(context.Context, uuid.UUID) (domain.PaymentAttempt, error) {
				t.Fatal("mark processing should not be called for failed replay")
				return domain.PaymentAttempt{}, nil
			},
			markSucceededFunc: func(context.Context, uuid.UUID, string) (domain.PaymentAttempt, error) {
				t.Fatal("mark succeeded should not be called for failed replay")
				return domain.PaymentAttempt{}, nil
			},
			markFailedFunc: func(context.Context, uuid.UUID, string, string) (domain.PaymentAttempt, error) {
				t.Fatal("mark failed should not be called for failed replay")
				return domain.PaymentAttempt{}, nil
			},
		}

		publisher := stubEventPublisher{publishFunc: func(context.Context, domain.DomainEvent) error {
			t.Fatal("publish should not be called for failed replay")
			return nil
		}}

		provider := stubPaymentProvider{chargeFunc: func(context.Context, outbound.ChargePaymentInput) (outbound.ChargePaymentResult, error) {
			t.Fatal("charge should not be called for failed replay")
			return outbound.ChargePaymentResult{}, nil
		}}

		svc := NewService(repo, publisher, provider, "payment-svc")

		got, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{
			OrderID:        orderID,
			Amount:         100,
			Currency:       "USD",
			ProviderName:   "stub",
			IdempotencyKey: "id-failed-replay",
		})

		require.NoError(t, err)
		require.Equal(t, existing, got.PaymentAttempt)
	})

	t.Run("continues flow for duplicate create when initiated", func(t *testing.T) {
		orderID := uuid.New()
		attemptID := uuid.New()
		existing := domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusInitiated, Amount: 200, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-retry"}
		processing := domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusProcessing, Amount: 200, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-retry"}
		succeeded := domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusSucceeded, ProviderReference: "stub-ok"}
		publisher := &capturingEventPublisher{}

		svc := NewService(stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{}, outbound.ErrPaymentAttemptDuplicate
			},
			getByOrderIDAndIdempotencyKeyFunc: func(context.Context, uuid.UUID, string) (domain.PaymentAttempt, error) {
				return existing, nil
			},
			markProcessingFunc: func(_ context.Context, paymentAttemptID uuid.UUID) (domain.PaymentAttempt, error) {
				require.Equal(t, attemptID, paymentAttemptID)
				return processing, nil
			},
			markSucceededFunc: func(_ context.Context, paymentAttemptID uuid.UUID, providerReference string) (domain.PaymentAttempt, error) {
				require.Equal(t, attemptID, paymentAttemptID)
				require.Equal(t, "stub-ok", providerReference)
				return succeeded, nil
			},
		}, publisher, stubPaymentProvider{chargeFunc: func(_ context.Context, input outbound.ChargePaymentInput) (outbound.ChargePaymentResult, error) {
			require.Equal(t, attemptID, input.PaymentAttemptID)
			return outbound.ChargePaymentResult{ProviderReference: "stub-ok"}, nil
		}}, "payment-svc")

		got, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: orderID, Amount: 200, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-retry"})

		require.NoError(t, err)
		require.Equal(t, succeeded, got.PaymentAttempt)
		require.Len(t, publisher.events, 1)
		require.Equal(t, "payment.succeeded", publisher.events[0].EventName)
	})

	t.Run("returns existing attempt for duplicate create when processing without second charge", func(t *testing.T) {
		orderID := uuid.New()
		existing := domain.PaymentAttempt{
			PaymentAttemptID: uuid.New(),
			OrderID:          orderID,
			Status:           domain.PaymentStatusProcessing,
			Amount:           200,
			Currency:         "USD",
			ProviderName:     "stub",
			IdempotencyKey:   "id-processing",
		}

		repo := stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{}, outbound.ErrPaymentAttemptDuplicate
			},
			getByOrderIDAndIdempotencyKeyFunc: func(context.Context, uuid.UUID, string) (domain.PaymentAttempt, error) {
				return existing, nil
			},
			markProcessingFunc: func(context.Context, uuid.UUID) (domain.PaymentAttempt, error) {
				t.Fatal("mark processing should not be called for processing replay")
				return domain.PaymentAttempt{}, nil
			},
		}

		publisher := &capturingEventPublisher{}

		svc := NewService(repo, publisher, stubPaymentProvider{}, "payment-svc")

		got, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{
			OrderID:        orderID,
			Amount:         200,
			Currency:       "USD",
			ProviderName:   "stub",
			IdempotencyKey: "id-processing",
		})

		require.NoError(t, err)
		require.Equal(t, existing, got.PaymentAttempt)
		require.Empty(t, publisher.events)
	})

	t.Run("maps outbound invalid arg error", func(t *testing.T) {
		svc := NewService(stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{}, outbound.ErrInvalidPaymentAttemptArg
			},
		}, stubEventPublisher{}, stubPaymentProvider{}, "payment-svc")

		_, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: uuid.New(), Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-bad"})

		require.ErrorIs(t, err, ErrInvalidPaymentInput)
	})

	t.Run("marks failed on decline", func(t *testing.T) {
		orderID := uuid.New()
		attemptID := uuid.New()
		processedAt := time.Now().UTC()
		failed := domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusFailed, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-fail", FailureCode: "declined", FailureMessage: "stub decline", ProcessedAt: &processedAt}
		publisher := &capturingEventPublisher{}

		svc := NewService(stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusInitiated, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-fail"}, nil
			},
			markProcessingFunc: func(context.Context, uuid.UUID) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{PaymentAttemptID: attemptID, OrderID: orderID, Status: domain.PaymentStatusProcessing, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-fail"}, nil
			},
			markFailedFunc: func(_ context.Context, paymentAttemptID uuid.UUID, failureCode string, failureMessage string) (domain.PaymentAttempt, error) {
				require.Equal(t, attemptID, paymentAttemptID)
				require.Equal(t, "declined", failureCode)
				require.Equal(t, "stub decline", failureMessage)
				return failed, nil
			},
		}, publisher, stubPaymentProvider{chargeFunc: func(context.Context, outbound.ChargePaymentInput) (outbound.ChargePaymentResult, error) {
			return outbound.ChargePaymentResult{FailureCode: "declined", FailureMessage: "stub decline"}, outbound.ErrPaymentDeclined
		}}, "payment-svc")

		got, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: orderID, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-fail"})

		require.NoError(t, err)
		require.Equal(t, failed, got.PaymentAttempt)
		require.Len(t, publisher.events, 2)
		require.Equal(t, "payment.initiated", publisher.events[0].EventName)
		require.Equal(t, "payment.failed", publisher.events[1].EventName)
	})
}

type capturingEventPublisher struct {
	events []domain.DomainEvent
}

func (p *capturingEventPublisher) Publish(_ context.Context, event domain.DomainEvent) error {
	p.events = append(p.events, event)
	return nil
}

type stubEventPublisher struct {
	publishFunc func(context.Context, domain.DomainEvent) error
}

func (s stubEventPublisher) Publish(ctx context.Context, event domain.DomainEvent) error {
	if s.publishFunc == nil {
		return nil
	}

	return s.publishFunc(ctx, event)
}

type stubPaymentAttemptRepository struct {
	createInitiatedFunc               func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error)
	getByOrderIDAndIdempotencyKeyFunc func(context.Context, uuid.UUID, string) (domain.PaymentAttempt, error)
	markProcessingFunc                func(context.Context, uuid.UUID) (domain.PaymentAttempt, error)
	markSucceededFunc                 func(context.Context, uuid.UUID, string) (domain.PaymentAttempt, error)
	markFailedFunc                    func(context.Context, uuid.UUID, string, string) (domain.PaymentAttempt, error)
}

func (s stubPaymentAttemptRepository) CreateInitiated(ctx context.Context, input outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
	if s.createInitiatedFunc == nil {
		return domain.PaymentAttempt{}, errors.New("create initiated not configured")
	}

	return s.createInitiatedFunc(ctx, input)
}

func (s stubPaymentAttemptRepository) GetByOrderIDAndIdempotencyKey(ctx context.Context, orderID uuid.UUID, idempotencyKey string) (domain.PaymentAttempt, error) {
	if s.getByOrderIDAndIdempotencyKeyFunc == nil {
		return domain.PaymentAttempt{}, errors.New("get by order id and idempotency key is not configured")
	}

	return s.getByOrderIDAndIdempotencyKeyFunc(ctx, orderID, idempotencyKey)
}

func (s stubPaymentAttemptRepository) MarkProcessing(ctx context.Context, paymentAttemptID uuid.UUID) (domain.PaymentAttempt, error) {
	if s.markProcessingFunc == nil {
		return domain.PaymentAttempt{}, errors.New("mark processing is not configured")
	}

	return s.markProcessingFunc(ctx, paymentAttemptID)
}

func (s stubPaymentAttemptRepository) MarkSucceeded(ctx context.Context, paymentAttemptID uuid.UUID, providerReference string) (domain.PaymentAttempt, error) {
	if s.markSucceededFunc == nil {
		return domain.PaymentAttempt{}, errors.New("mark succeeded is not configured")
	}

	return s.markSucceededFunc(ctx, paymentAttemptID, providerReference)
}

func (s stubPaymentAttemptRepository) MarkFailed(ctx context.Context, paymentAttemptID uuid.UUID, failureCode string, failureMessage string) (domain.PaymentAttempt, error) {
	if s.markFailedFunc == nil {
		return domain.PaymentAttempt{}, errors.New("mark failed is not configured")
	}

	return s.markFailedFunc(ctx, paymentAttemptID, failureCode, failureMessage)
}

type stubPaymentProvider struct {
	chargeFunc func(context.Context, outbound.ChargePaymentInput) (outbound.ChargePaymentResult, error)
}

func (s stubPaymentProvider) Charge(ctx context.Context, input outbound.ChargePaymentInput) (outbound.ChargePaymentResult, error) {
	if s.chargeFunc == nil {
		return outbound.ChargePaymentResult{}, errors.New("charge is not configured")
	}

	return s.chargeFunc(ctx, input)
}

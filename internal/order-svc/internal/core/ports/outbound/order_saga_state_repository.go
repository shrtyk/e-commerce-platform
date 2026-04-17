package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrOrderSagaStateInvalidTransition = errors.New("order saga state invalid transition")
)

//mockery:generate: true
type OrderSagaStateRepository interface {
	TransitionStockStageToRequested(ctx context.Context, orderID uuid.UUID) (SagaState, error)
	TransitionStockStageToSucceeded(ctx context.Context, orderID uuid.UUID) (SagaState, error)
	TransitionStockStageToFailed(ctx context.Context, orderID uuid.UUID) (SagaState, error)

	TransitionPaymentStageToRequested(ctx context.Context, orderID uuid.UUID) (SagaState, error)
	TransitionPaymentStageToSucceeded(ctx context.Context, orderID uuid.UUID) (SagaState, error)
	TransitionPaymentStageToFailed(ctx context.Context, orderID uuid.UUID) (SagaState, error)

	SetLastErrorCode(ctx context.Context, orderID uuid.UUID, lastErrorCode string) (SagaState, error)
	ClearLastErrorCode(ctx context.Context, orderID uuid.UUID) (SagaState, error)
}

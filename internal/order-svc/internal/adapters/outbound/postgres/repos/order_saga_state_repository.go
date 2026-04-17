package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

type OrderSagaStateRepository struct {
	queries sqlc.Querier
}

func NewOrderSagaStateRepository(db *sql.DB) *OrderSagaStateRepository {
	return NewOrderSagaStateRepositoryFromQuerier(sqlc.New(db))
}

func NewOrderSagaStateRepositoryFromQuerier(queries sqlc.Querier) *OrderSagaStateRepository {
	return &OrderSagaStateRepository{queries: queries}
}

func NewOrderSagaStateRepositoryFromTx(tx *sql.Tx) *OrderSagaStateRepository {
	return NewOrderSagaStateRepositoryFromQuerier(sqlc.New(tx))
}

func (r *OrderSagaStateRepository) TransitionStockStageToRequested(ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
	state, err := r.queries.MarkOrderSagaStockRequested(ctx, orderID)
	if err != nil {
		return outbound.SagaState{}, r.mapTransitionErr(ctx, orderID, err, "transition stock stage to requested")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) TransitionStockStageToSucceeded(ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
	state, err := r.queries.MarkOrderSagaStockSucceeded(ctx, orderID)
	if err != nil {
		return outbound.SagaState{}, r.mapTransitionErr(ctx, orderID, err, "transition stock stage to succeeded")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) TransitionStockStageToFailed(ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
	state, err := r.queries.MarkOrderSagaStockFailed(ctx, orderID)
	if err != nil {
		return outbound.SagaState{}, r.mapTransitionErr(ctx, orderID, err, "transition stock stage to failed")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) TransitionPaymentStageToRequested(ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
	state, err := r.queries.MarkOrderSagaPaymentRequested(ctx, orderID)
	if err != nil {
		return outbound.SagaState{}, r.mapTransitionErr(ctx, orderID, err, "transition payment stage to requested")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) TransitionPaymentStageToSucceeded(ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
	state, err := r.queries.MarkOrderSagaPaymentSucceeded(ctx, orderID)
	if err != nil {
		return outbound.SagaState{}, r.mapTransitionErr(ctx, orderID, err, "transition payment stage to succeeded")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) TransitionPaymentStageToFailed(ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
	state, err := r.queries.MarkOrderSagaPaymentFailed(ctx, orderID)
	if err != nil {
		return outbound.SagaState{}, r.mapTransitionErr(ctx, orderID, err, "transition payment stage to failed")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) SetLastErrorCode(
	ctx context.Context,
	orderID uuid.UUID,
	lastErrorCode string,
) (outbound.SagaState, error) {
	state, err := r.queries.SetOrderSagaLastErrorCode(ctx, sqlc.SetOrderSagaLastErrorCodeParams{
		OrderID:       orderID,
		LastErrorCode: sql.NullString{String: lastErrorCode, Valid: true},
	})
	if err != nil {
		return outbound.SagaState{}, mapNotFoundErr(err, "set order saga last error code")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) ClearLastErrorCode(ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
	state, err := r.queries.ClearOrderSagaLastErrorCode(ctx, orderID)
	if err != nil {
		return outbound.SagaState{}, mapNotFoundErr(err, "clear order saga last error code")
	}

	return mapSagaState(state), nil
}

func (r *OrderSagaStateRepository) mapTransitionErr(
	ctx context.Context,
	orderID uuid.UUID,
	err error,
	operation string,
) error {
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, err)
	}

	_, getErr := r.queries.GetOrderSagaStateByOrderID(ctx, orderID)
	if getErr == nil {
		return outbound.ErrOrderSagaStateInvalidTransition
	}

	if errors.Is(getErr, sql.ErrNoRows) {
		return outbound.ErrOrderNotFound
	}

	return fmt.Errorf("get order saga state by order id: %w", getErr)
}

func mapNotFoundErr(err error, operation string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return outbound.ErrOrderNotFound
	}

	return fmt.Errorf("%s: %w", operation, err)
}

func mapSagaState(state sqlc.OrderSagaState) outbound.SagaState {
	result := outbound.SagaState{
		OrderID:      state.OrderID,
		StockStage:   outbound.SagaStage(state.StockStage),
		PaymentStage: outbound.SagaStage(state.PaymentStage),
		CreatedAt:    state.CreatedAt,
		UpdatedAt:    state.UpdatedAt,
	}
	if state.LastErrorCode.Valid {
		lastErrorCode := state.LastErrorCode.String
		result.LastErrorCode = &lastErrorCode
	}

	return result
}

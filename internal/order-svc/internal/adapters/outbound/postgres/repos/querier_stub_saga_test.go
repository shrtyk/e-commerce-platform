package repos

import (
	"context"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
)

func (s stubQuerier) CreateOrderSagaState(ctx context.Context, arg sqlc.CreateOrderSagaStateParams) (sqlc.OrderSagaState, error) {
	if s.saga.createOrderSagaStateFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("CreateOrderSagaState")
	}

	return s.saga.createOrderSagaStateFunc(ctx, arg)
}

func (s stubQuerier) GetOrderSagaStateByOrderID(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.getOrderSagaStateByOrderIDFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("GetOrderSagaStateByOrderID")
	}

	return s.saga.getOrderSagaStateByOrderIDFunc(ctx, orderID)
}

func (s stubQuerier) SetOrderSagaLastErrorCode(ctx context.Context, arg sqlc.SetOrderSagaLastErrorCodeParams) (sqlc.OrderSagaState, error) {
	if s.saga.setOrderSagaLastErrorCodeFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("SetOrderSagaLastErrorCode")
	}

	return s.saga.setOrderSagaLastErrorCodeFunc(ctx, arg)
}

func (s stubQuerier) ClearOrderSagaLastErrorCode(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.clearOrderSagaLastErrorCodeFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("ClearOrderSagaLastErrorCode")
	}

	return s.saga.clearOrderSagaLastErrorCodeFunc(ctx, orderID)
}

func (s stubQuerier) MarkOrderSagaStockRequested(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.markOrderSagaStockRequestedFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("MarkOrderSagaStockRequested")
	}

	return s.saga.markOrderSagaStockRequestedFunc(ctx, orderID)
}

func (s stubQuerier) MarkOrderSagaStockSucceeded(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.markOrderSagaStockSucceededFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("MarkOrderSagaStockSucceeded")
	}

	return s.saga.markOrderSagaStockSucceededFunc(ctx, orderID)
}

func (s stubQuerier) MarkOrderSagaStockFailed(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.markOrderSagaStockFailedFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("MarkOrderSagaStockFailed")
	}

	return s.saga.markOrderSagaStockFailedFunc(ctx, orderID)
}

func (s stubQuerier) MarkOrderSagaPaymentRequested(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.markOrderSagaPaymentRequestedFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("MarkOrderSagaPaymentRequested")
	}

	return s.saga.markOrderSagaPaymentRequestedFunc(ctx, orderID)
}

func (s stubQuerier) MarkOrderSagaPaymentSucceeded(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.markOrderSagaPaymentSucceededFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("MarkOrderSagaPaymentSucceeded")
	}

	return s.saga.markOrderSagaPaymentSucceededFunc(ctx, orderID)
}

func (s stubQuerier) MarkOrderSagaPaymentFailed(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error) {
	if s.saga.markOrderSagaPaymentFailedFunc == nil {
		return sqlc.OrderSagaState{}, unexpectedQuerierCall("MarkOrderSagaPaymentFailed")
	}

	return s.saga.markOrderSagaPaymentFailedFunc(ctx, orderID)
}

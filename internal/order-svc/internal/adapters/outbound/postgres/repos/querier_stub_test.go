package repos

import (
	"context"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
)

type stubQuerier struct {
	order        orderQuerierStub
	itemsHistory itemsHistoryQuerierStub
	saga         sagaQuerierStub
}

type orderQuerierStub struct {
	createOrderFunc                       func(ctx context.Context, arg sqlc.CreateOrderParams) (sqlc.Order, error)
	createOrderCheckoutIdempotencyFunc    func(ctx context.Context, arg sqlc.CreateOrderCheckoutIdempotencyParams) error
	getOrderByIDFunc                      func(ctx context.Context, orderID uuid.UUID) (sqlc.Order, error)
	getOrderByUserIDAndIdempotencyKeyFunc func(ctx context.Context, arg sqlc.GetOrderByUserIDAndIdempotencyKeyParams) (sqlc.Order, error)
	transitionOrderStatusFunc             func(ctx context.Context, arg sqlc.TransitionOrderStatusParams) (sqlc.Order, error)
}

type itemsHistoryQuerierStub struct {
	createOrderItemFunc          func(ctx context.Context, arg sqlc.CreateOrderItemParams) (sqlc.OrderItem, error)
	listOrderItemsByOrderIDFunc  func(ctx context.Context, orderID uuid.UUID) ([]sqlc.OrderItem, error)
	appendOrderStatusHistoryFunc func(ctx context.Context, arg sqlc.AppendOrderStatusHistoryParams) (sqlc.OrderStatusHistory, error)
}

type sagaQuerierStub struct {
	createOrderSagaStateFunc          func(ctx context.Context, arg sqlc.CreateOrderSagaStateParams) (sqlc.OrderSagaState, error)
	getOrderSagaStateByOrderIDFunc    func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
	setOrderSagaLastErrorCodeFunc     func(ctx context.Context, arg sqlc.SetOrderSagaLastErrorCodeParams) (sqlc.OrderSagaState, error)
	clearOrderSagaLastErrorCodeFunc   func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
	markOrderSagaStockRequestedFunc   func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
	markOrderSagaStockSucceededFunc   func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
	markOrderSagaStockFailedFunc      func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
	markOrderSagaPaymentRequestedFunc func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
	markOrderSagaPaymentSucceededFunc func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
	markOrderSagaPaymentFailedFunc    func(ctx context.Context, orderID uuid.UUID) (sqlc.OrderSagaState, error)
}

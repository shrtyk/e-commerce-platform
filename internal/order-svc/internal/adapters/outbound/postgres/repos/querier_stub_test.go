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
	outbox       outboxQuerierStub
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

type outboxQuerierStub struct {
	appendOutboxRecordFunc                func(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error)
	claimPendingOutboxRecordsFunc         func(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	claimStaleInProgressOutboxRecordsFunc func(ctx context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	markOutboxRecordPublishedFunc         func(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error)
	markOutboxRecordRetryableFailureFunc  func(ctx context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error)
	markOutboxRecordDeadFunc              func(ctx context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error)
}

func (s stubQuerier) AppendOutboxRecord(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error) {
	if s.outbox.appendOutboxRecordFunc == nil {
		return sqlc.OutboxRecord{}, unexpectedQuerierCall("AppendOutboxRecord")
	}

	return s.outbox.appendOutboxRecordFunc(ctx, arg)
}

func (s stubQuerier) ClaimPendingOutboxRecords(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
	if s.outbox.claimPendingOutboxRecordsFunc == nil {
		return nil, unexpectedQuerierCall("ClaimPendingOutboxRecords")
	}

	return s.outbox.claimPendingOutboxRecordsFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordPublished(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
	if s.outbox.markOutboxRecordPublishedFunc == nil {
		return 0, unexpectedQuerierCall("MarkOutboxRecordPublished")
	}

	return s.outbox.markOutboxRecordPublishedFunc(ctx, arg)
}

func (s stubQuerier) ClaimStaleInProgressOutboxRecords(ctx context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
	if s.outbox.claimStaleInProgressOutboxRecordsFunc == nil {
		return nil, unexpectedQuerierCall("ClaimStaleInProgressOutboxRecords")
	}

	return s.outbox.claimStaleInProgressOutboxRecordsFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordRetryableFailure(ctx context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error) {
	if s.outbox.markOutboxRecordRetryableFailureFunc == nil {
		return 0, unexpectedQuerierCall("MarkOutboxRecordRetryableFailure")
	}

	return s.outbox.markOutboxRecordRetryableFailureFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordDead(ctx context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error) {
	if s.outbox.markOutboxRecordDeadFunc == nil {
		return 0, unexpectedQuerierCall("MarkOutboxRecordDead")
	}

	return s.outbox.markOutboxRecordDeadFunc(ctx, arg)
}

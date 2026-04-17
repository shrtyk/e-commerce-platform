package repos

import (
	"context"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
)

func (s stubQuerier) CreateOrderItem(ctx context.Context, arg sqlc.CreateOrderItemParams) (sqlc.OrderItem, error) {
	if s.itemsHistory.createOrderItemFunc == nil {
		return sqlc.OrderItem{}, unexpectedQuerierCall("CreateOrderItem")
	}

	return s.itemsHistory.createOrderItemFunc(ctx, arg)
}

func (s stubQuerier) ListOrderItemsByOrderID(ctx context.Context, orderID uuid.UUID) ([]sqlc.OrderItem, error) {
	if s.itemsHistory.listOrderItemsByOrderIDFunc == nil {
		return nil, unexpectedQuerierCall("ListOrderItemsByOrderID")
	}

	return s.itemsHistory.listOrderItemsByOrderIDFunc(ctx, orderID)
}

func (s stubQuerier) AppendOrderStatusHistory(ctx context.Context, arg sqlc.AppendOrderStatusHistoryParams) (sqlc.OrderStatusHistory, error) {
	if s.itemsHistory.appendOrderStatusHistoryFunc == nil {
		return sqlc.OrderStatusHistory{}, unexpectedQuerierCall("AppendOrderStatusHistory")
	}

	return s.itemsHistory.appendOrderStatusHistoryFunc(ctx, arg)
}

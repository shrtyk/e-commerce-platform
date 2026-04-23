package repos

import (
	"context"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
)

func (s stubQuerier) CreateOrder(ctx context.Context, arg sqlc.CreateOrderParams) (sqlc.Order, error) {
	if s.order.createOrderFunc == nil {
		return sqlc.Order{}, unexpectedQuerierCall("CreateOrder")
	}

	return s.order.createOrderFunc(ctx, arg)
}

func (s stubQuerier) CreateOrderCheckoutIdempotency(ctx context.Context, arg sqlc.CreateOrderCheckoutIdempotencyParams) error {
	if s.order.createOrderCheckoutIdempotencyFunc == nil {
		return unexpectedQuerierCall("CreateOrderCheckoutIdempotency")
	}

	return s.order.createOrderCheckoutIdempotencyFunc(ctx, arg)
}

func (s stubQuerier) CreateConsumerIdempotency(ctx context.Context, arg sqlc.CreateConsumerIdempotencyParams) error {
	if s.order.createConsumerIdempotencyFunc == nil {
		return unexpectedQuerierCall("CreateConsumerIdempotency")
	}

	return s.order.createConsumerIdempotencyFunc(ctx, arg)
}

func (s stubQuerier) ConsumerIdempotencyExists(ctx context.Context, arg sqlc.ConsumerIdempotencyExistsParams) (bool, error) {
	if s.order.consumerIdempotencyExistsFunc == nil {
		return false, unexpectedQuerierCall("ConsumerIdempotencyExists")
	}

	return s.order.consumerIdempotencyExistsFunc(ctx, arg)
}

func (s stubQuerier) GetOrderByID(ctx context.Context, orderID uuid.UUID) (sqlc.Order, error) {
	if s.order.getOrderByIDFunc == nil {
		return sqlc.Order{}, unexpectedQuerierCall("GetOrderByID")
	}

	return s.order.getOrderByIDFunc(ctx, orderID)
}

func (s stubQuerier) GetCheckoutIdempotencyPayloadFingerprint(ctx context.Context, arg sqlc.GetCheckoutIdempotencyPayloadFingerprintParams) (string, error) {
	if s.order.getCheckoutIdempotencyPayloadFingerprintFunc == nil {
		return "", unexpectedQuerierCall("GetCheckoutIdempotencyPayloadFingerprint")
	}

	return s.order.getCheckoutIdempotencyPayloadFingerprintFunc(ctx, arg)
}

func (s stubQuerier) GetOrderByUserIDAndIdempotencyKey(ctx context.Context, arg sqlc.GetOrderByUserIDAndIdempotencyKeyParams) (sqlc.Order, error) {
	if s.order.getOrderByUserIDAndIdempotencyKeyFunc == nil {
		return sqlc.Order{}, unexpectedQuerierCall("GetOrderByUserIDAndIdempotencyKey")
	}

	return s.order.getOrderByUserIDAndIdempotencyKeyFunc(ctx, arg)
}

func (s stubQuerier) TransitionOrderStatus(ctx context.Context, arg sqlc.TransitionOrderStatusParams) (sqlc.Order, error) {
	if s.order.transitionOrderStatusFunc == nil {
		return sqlc.Order{}, unexpectedQuerierCall("TransitionOrderStatus")
	}

	return s.order.transitionOrderStatusFunc(ctx, arg)
}

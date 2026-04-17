package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

type OrderRepository struct {
	db      *sql.DB
	queries sqlc.Querier
}

func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db, queries: sqlc.New(db)}
}

func NewOrderRepositoryFromQuerier(queries sqlc.Querier) *OrderRepository {
	return &OrderRepository{queries: queries}
}

func NewOrderRepositoryFromTx(tx *sql.Tx) *OrderRepository {
	return NewOrderRepositoryFromQuerier(sqlc.New(tx))
}

func (r *OrderRepository) CreateWithItems(ctx context.Context, input outbound.CreateOrderInput) (outbound.Order, error) {
	if r.db == nil {
		return r.createWithItems(ctx, r.queries, input)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return outbound.Order{}, fmt.Errorf("begin create order transaction: %w", err)
	}

	queries := sqlc.New(tx)
	order, err := r.createWithItems(ctx, queries, input)
	if err != nil {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil {
			return outbound.Order{}, fmt.Errorf("rollback create order transaction: %w", errors.Join(err, rollbackErr))
		}

		return outbound.Order{}, err
	}

	if err := tx.Commit(); err != nil {
		return outbound.Order{}, fmt.Errorf("commit create order transaction: %w", err)
	}

	return order, nil
}

func (r *OrderRepository) createWithItems(
	ctx context.Context,
	queries sqlc.Querier,
	input outbound.CreateOrderInput,
) (outbound.Order, error) {
	createdOrder, err := queries.CreateOrder(ctx, sqlc.CreateOrderParams{
		OrderID:     input.OrderID,
		UserID:      input.UserID,
		Status:      sqlc.OrderStatus(input.Status),
		Currency:    input.Currency,
		TotalAmount: input.TotalAmount,
	})
	if err != nil {
		return outbound.Order{}, fmt.Errorf("create order: %w", mapOrderWriteErr(err))
	}

	items := make([]outbound.OrderItem, 0, len(input.Items))
	for _, item := range input.Items {
		createdItem, itemErr := queries.CreateOrderItem(ctx, sqlc.CreateOrderItemParams{
			OrderID:   createdOrder.OrderID,
			ProductID: item.ProductID,
			Sku:       item.SKU,
			Name:      item.Name,
			Quantity:  item.Quantity,
			UnitPrice: item.UnitPrice,
			LineTotal: item.LineTotal,
			Currency:  item.Currency,
		})
		if itemErr != nil {
			return outbound.Order{}, fmt.Errorf("create order item: %w", mapOrderWriteErr(itemErr))
		}

		items = append(items, mapOrderItem(createdItem))
	}

	_, err = queries.CreateOrderSagaState(ctx, sqlc.CreateOrderSagaStateParams{
		OrderID:      createdOrder.OrderID,
		StockStage:   sqlc.OrderSagaStage(outbound.SagaStageNotStarted),
		PaymentStage: sqlc.OrderSagaStage(outbound.SagaStageNotStarted),
	})
	if err != nil {
		return outbound.Order{}, fmt.Errorf("create order saga state: %w", mapOrderWriteErr(err))
	}

	err = queries.CreateOrderCheckoutIdempotency(ctx, sqlc.CreateOrderCheckoutIdempotencyParams{
		OrderID:        createdOrder.OrderID,
		UserID:         input.UserID,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return outbound.Order{}, fmt.Errorf("create order idempotency key: %w", mapOrderWriteErr(err))
	}

	return mapOrder(createdOrder, items), nil
}

func (r *OrderRepository) GetByID(ctx context.Context, orderID uuid.UUID) (outbound.Order, error) {
	orderRow, err := r.queries.GetOrderByID(ctx, orderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return outbound.Order{}, outbound.ErrOrderNotFound
		}

		return outbound.Order{}, fmt.Errorf("get order by id %q: %w", orderID.String(), err)
	}

	itemsRows, err := r.queries.ListOrderItemsByOrderID(ctx, orderID)
	if err != nil {
		return outbound.Order{}, fmt.Errorf("list order items by order id %q: %w", orderID.String(), err)
	}

	return mapOrder(orderRow, mapOrderItems(itemsRows)), nil
}

func (r *OrderRepository) GetByUserIDAndIdempotencyKey(
	ctx context.Context,
	userID uuid.UUID,
	idempotencyKey string,
) (outbound.Order, error) {
	orderRow, err := r.queries.GetOrderByUserIDAndIdempotencyKey(ctx, sqlc.GetOrderByUserIDAndIdempotencyKeyParams{
		UserID:         userID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return outbound.Order{}, outbound.ErrOrderNotFound
		}

		return outbound.Order{}, fmt.Errorf("get order by user id %q and idempotency key: %w", userID.String(), err)
	}

	itemsRows, err := r.queries.ListOrderItemsByOrderID(ctx, orderRow.OrderID)
	if err != nil {
		return outbound.Order{}, fmt.Errorf("list order items by order id %q: %w", orderRow.OrderID.String(), err)
	}

	return mapOrder(orderRow, mapOrderItems(itemsRows)), nil
}

func (r *OrderRepository) TransitionStatus(
	ctx context.Context,
	orderID uuid.UUID,
	fromStatus outbound.OrderStatus,
	toStatus outbound.OrderStatus,
) (outbound.Order, error) {
	updated, err := r.queries.TransitionOrderStatus(ctx, sqlc.TransitionOrderStatusParams{
		ToStatus:   sqlc.OrderStatus(toStatus),
		OrderID:    orderID,
		FromStatus: sqlc.OrderStatus(fromStatus),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, getErr := r.queries.GetOrderByID(ctx, orderID)
			if getErr != nil {
				if errors.Is(getErr, sql.ErrNoRows) {
					return outbound.Order{}, outbound.ErrOrderNotFound
				}

				return outbound.Order{}, fmt.Errorf("check order exists before transition: %w", getErr)
			}

			return outbound.Order{}, outbound.ErrOrderInvalidStatusTransition
		}

		return outbound.Order{}, fmt.Errorf("transition order status: %w", mapOrderWriteErr(err))
	}

	return mapOrder(updated, nil), nil
}

func (r *OrderRepository) AppendStatusHistory(
	ctx context.Context,
	orderID uuid.UUID,
	fromStatus *outbound.OrderStatus,
	toStatus outbound.OrderStatus,
	reasonCode *string,
) (outbound.OrderStatusHistory, error) {
	fromStatusArg := sql.NullString{}
	if fromStatus != nil {
		fromStatusArg = sql.NullString{String: string(*fromStatus), Valid: true}
	}

	reasonCodeArg := sql.NullString{}
	if reasonCode != nil {
		reasonCodeArg = sql.NullString{String: *reasonCode, Valid: true}
	}

	entry, err := r.queries.AppendOrderStatusHistory(ctx, sqlc.AppendOrderStatusHistoryParams{
		OrderID:    orderID,
		FromStatus: fromStatusArg,
		ToStatus:   string(toStatus),
		ReasonCode: reasonCodeArg,
	})
	if err != nil {
		return outbound.OrderStatusHistory{}, fmt.Errorf("append order status history: %w", mapOrderWriteErr(err))
	}

	return mapOrderStatusHistory(entry), nil
}

func mapOrderWriteErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23503":
			return outbound.ErrOrderNotFound
		case "23505":
			switch pgErr.ConstraintName {
			case "orders_pkey":
				return outbound.ErrOrderAlreadyExists
			case "uq_order_checkout_idempotency_user_key":
				return outbound.ErrOrderIdempotencyConflict
			case "uq_order_checkout_idempotency_order_id":
				return outbound.ErrOrderAlreadyExists
			}
		}
	}

	return err
}

func mapOrder(order sqlc.Order, items []outbound.OrderItem) outbound.Order {
	return outbound.Order{
		OrderID:     order.OrderID,
		UserID:      order.UserID,
		Status:      outbound.OrderStatus(order.Status),
		Currency:    order.Currency,
		TotalAmount: order.TotalAmount,
		CreatedAt:   order.CreatedAt,
		UpdatedAt:   order.UpdatedAt,
		Items:       items,
	}
}

func mapOrderItems(items []sqlc.OrderItem) []outbound.OrderItem {
	result := make([]outbound.OrderItem, 0, len(items))
	for _, item := range items {
		result = append(result, mapOrderItem(item))
	}

	return result
}

func mapOrderItem(item sqlc.OrderItem) outbound.OrderItem {
	return outbound.OrderItem{
		OrderItemID: item.OrderItemID,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
		SKU:         item.Sku,
		Name:        item.Name,
		Quantity:    item.Quantity,
		UnitPrice:   item.UnitPrice,
		LineTotal:   item.LineTotal,
		Currency:    item.Currency,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func mapOrderStatusHistory(entry sqlc.OrderStatusHistory) outbound.OrderStatusHistory {
	result := outbound.OrderStatusHistory{
		OrderStatusHistoryID: entry.OrderStatusHistoryID,
		OrderID:              entry.OrderID,
		ToStatus:             outbound.OrderStatus(entry.ToStatus),
		CreatedAt:            entry.CreatedAt,
	}
	if entry.FromStatus.Valid {
		fromStatus := outbound.OrderStatus(entry.FromStatus.String)
		result.FromStatus = &fromStatus
	}
	if entry.ReasonCode.Valid {
		reasonCode := entry.ReasonCode.String
		result.ReasonCode = &reasonCode
	}

	return result
}

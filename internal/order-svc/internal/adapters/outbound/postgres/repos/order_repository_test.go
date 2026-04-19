package repos

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

func TestOrderRepositoryGetByUserIDAndIdempotencyKey(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	orderID := uuid.New()
	userID := uuid.New()
	idempotencyKey := "idem-1"

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
		itemsLen  int
	}{
		{
			name: "success",
			stub: stubQuerier{
				order: orderQuerierStub{
					getOrderByUserIDAndIdempotencyKeyFunc: func(_ context.Context, arg sqlc.GetOrderByUserIDAndIdempotencyKeyParams) (sqlc.Order, error) {
						require.Equal(t, userID, arg.UserID)
						require.Equal(t, idempotencyKey, arg.IdempotencyKey)

						return sqlc.Order{
							OrderID:     orderID,
							UserID:      userID,
							Status:      sqlc.OrderStatus(outbound.OrderStatusPending),
							Currency:    "USD",
							TotalAmount: 300,
							CreatedAt:   now,
							UpdatedAt:   now,
						}, nil
					},
				},
				itemsHistory: itemsHistoryQuerierStub{
					listOrderItemsByOrderIDFunc: func(_ context.Context, gotOrderID uuid.UUID) ([]sqlc.OrderItem, error) {
						require.Equal(t, orderID, gotOrderID)
						return []sqlc.OrderItem{{
							OrderItemID: uuid.New(),
							OrderID:     orderID,
							ProductID:   uuid.New(),
							Sku:         "SKU-1",
							Name:        "Name",
							Quantity:    1,
							UnitPrice:   300,
							LineTotal:   300,
							Currency:    "USD",
							CreatedAt:   now,
							UpdatedAt:   now,
						}}, nil
					},
				},
			},
			itemsLen: 1,
		},
		{
			name: "not found",
			stub: stubQuerier{
				order: orderQuerierStub{
					getOrderByUserIDAndIdempotencyKeyFunc: func(_ context.Context, _ sqlc.GetOrderByUserIDAndIdempotencyKeyParams) (sqlc.Order, error) {
						return sqlc.Order{}, sql.ErrNoRows
					},
				},
			},
			errIs: outbound.ErrOrderNotFound,
		},
		{
			name: "query error",
			stub: stubQuerier{
				order: orderQuerierStub{
					getOrderByUserIDAndIdempotencyKeyFunc: func(_ context.Context, _ sqlc.GetOrderByUserIDAndIdempotencyKeyParams) (sqlc.Order, error) {
						return sqlc.Order{}, sql.ErrConnDone
					},
				},
			},
			errPrefix: "get order by user id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewOrderRepositoryFromTransactionalQuerier(tt.stub)

			order, err := repo.GetByUserIDAndIdempotencyKey(context.Background(), userID, idempotencyKey)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, order)
				return
			}

			require.NoError(t, err)
			require.Equal(t, orderID, order.OrderID)
			require.Len(t, order.Items, tt.itemsLen)
		})
	}
}

func TestOrderRepositoryCreateWithItemsDuplicateIdempotency(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	orderID := uuid.New()
	userID := uuid.New()

	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		order: orderQuerierStub{
			createOrderFunc: func(_ context.Context, arg sqlc.CreateOrderParams) (sqlc.Order, error) {
				require.Equal(t, orderID, arg.OrderID)
				require.Equal(t, userID, arg.UserID)
				return sqlc.Order{
					OrderID:     orderID,
					UserID:      userID,
					Status:      sqlc.OrderStatus(outbound.OrderStatusPending),
					Currency:    "USD",
					TotalAmount: 500,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
			createOrderCheckoutIdempotencyFunc: func(_ context.Context, _ sqlc.CreateOrderCheckoutIdempotencyParams) error {
				return &pgconn.PgError{Code: "23505", ConstraintName: "uq_order_checkout_idempotency_user_key"}
			},
		},
		itemsHistory: itemsHistoryQuerierStub{
			createOrderItemFunc: func(_ context.Context, arg sqlc.CreateOrderItemParams) (sqlc.OrderItem, error) {
				return sqlc.OrderItem{
					OrderItemID: uuid.New(),
					OrderID:     arg.OrderID,
					ProductID:   arg.ProductID,
					Sku:         arg.Sku,
					Name:        arg.Name,
					Quantity:    arg.Quantity,
					UnitPrice:   arg.UnitPrice,
					LineTotal:   arg.LineTotal,
					Currency:    arg.Currency,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
		},
		saga: sagaQuerierStub{
			createOrderSagaStateFunc: func(_ context.Context, arg sqlc.CreateOrderSagaStateParams) (sqlc.OrderSagaState, error) {
				return sqlc.OrderSagaState{
					OrderID:      arg.OrderID,
					StockStage:   arg.StockStage,
					PaymentStage: arg.PaymentStage,
					CreatedAt:    now,
					UpdatedAt:    now,
				}, nil
			},
		},
	})

	order, err := repo.CreateWithItems(context.Background(), outbound.CreateOrderInput{
		OrderID:        orderID,
		UserID:         userID,
		Status:         outbound.OrderStatusPending,
		Currency:       "USD",
		TotalAmount:    500,
		IdempotencyKey: "idem-duplicate",
		Items: []outbound.CreateOrderItemInput{{
			ProductID: uuid.New(),
			SKU:       "SKU-1",
			Name:      "Name",
			Quantity:  1,
			UnitPrice: 500,
			LineTotal: 500,
			Currency:  "USD",
		}},
	})

	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderIdempotencyConflict)
	require.ErrorContains(t, err, "create order idempotency key")
	require.Zero(t, order)
}

func TestOrderRepositoryCreateWithItemsStopsOnItemError(t *testing.T) {
	orderID := uuid.New()
	userID := uuid.New()

	sagaCalled := false
	idempotencyCalled := false

	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		order: orderQuerierStub{
			createOrderFunc: func(_ context.Context, _ sqlc.CreateOrderParams) (sqlc.Order, error) {
				return sqlc.Order{OrderID: orderID, UserID: userID}, nil
			},
			createOrderCheckoutIdempotencyFunc: func(_ context.Context, _ sqlc.CreateOrderCheckoutIdempotencyParams) error {
				idempotencyCalled = true
				return nil
			},
		},
		itemsHistory: itemsHistoryQuerierStub{
			createOrderItemFunc: func(_ context.Context, _ sqlc.CreateOrderItemParams) (sqlc.OrderItem, error) {
				return sqlc.OrderItem{}, sql.ErrConnDone
			},
		},
		saga: sagaQuerierStub{
			createOrderSagaStateFunc: func(_ context.Context, _ sqlc.CreateOrderSagaStateParams) (sqlc.OrderSagaState, error) {
				sagaCalled = true
				return sqlc.OrderSagaState{}, nil
			},
		},
	})

	order, err := repo.CreateWithItems(context.Background(), outbound.CreateOrderInput{
		OrderID:        orderID,
		UserID:         userID,
		Status:         outbound.OrderStatusPending,
		Currency:       "USD",
		TotalAmount:    500,
		IdempotencyKey: "idem-1",
		Items: []outbound.CreateOrderItemInput{{
			ProductID: uuid.New(),
			SKU:       "SKU-1",
			Name:      "Name",
			Quantity:  1,
			UnitPrice: 500,
			LineTotal: 500,
			Currency:  "USD",
		}},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "create order item")
	require.Zero(t, order)
	require.False(t, sagaCalled)
	require.False(t, idempotencyCalled)
}

func TestOrderRepositoryCreateWithItemsRequiresTransactionalRepository(t *testing.T) {
	repo := NewOrderRepositoryFromQuerier(stubQuerier{})

	created, err := repo.CreateWithItems(context.Background(), outbound.CreateOrderInput{})

	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderUnsafeCreateWithItems)
	require.Zero(t, created)
}

func TestOrderRepositoryCreateWithItemsTransactionPaths(t *testing.T) {
	orderID := uuid.New()
	userID := uuid.New()
	productID := uuid.New()
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	input := outbound.CreateOrderInput{
		OrderID:        orderID,
		UserID:         userID,
		Status:         outbound.OrderStatusPending,
		Currency:       "USD",
		TotalAmount:    500,
		IdempotencyKey: "idem-1",
		Items: []outbound.CreateOrderItemInput{{
			ProductID: productID,
			SKU:       "SKU-1",
			Name:      "Name",
			Quantity:  1,
			UnitPrice: 500,
			LineTotal: 500,
			Currency:  "USD",
		}},
	}

	t.Run("begin tx failure", func(t *testing.T) {
		behavior := &testSQLBehavior{beginErr: sql.ErrConnDone}
		db := newTestDB(t, behavior)
		repo := NewOrderRepository(db)

		created, err := repo.CreateWithItems(context.Background(), input)

		require.Error(t, err)
		require.ErrorContains(t, err, "begin create order transaction")
		require.ErrorIs(t, err, sql.ErrConnDone)
		require.Zero(t, created)
		require.Equal(t, 0, behavior.commitCount)
		require.Equal(t, 0, behavior.rollbackCount)
	})

	t.Run("rollback on mid-create failure", func(t *testing.T) {
		behavior := &testSQLBehavior{}
		behavior.onQuery = func(query string, _ []driver.Value) (testQueryResult, error) {
			switch {
			case queryHasName(query, "CreateOrder"):
				return testQueryResult{
					columns: []string{"order_id", "user_id", "status", "currency", "total_amount", "created_at", "updated_at"},
					rows: [][]driver.Value{{
						orderID,
						userID,
						string(outbound.OrderStatusPending),
						"USD",
						int64(500),
						now,
						now,
					}},
				}, nil
			case queryHasName(query, "CreateOrderItem"):
				return testQueryResult{}, sql.ErrConnDone
			default:
				return testQueryResult{}, fmt.Errorf("unexpected query: %s", query)
			}
		}

		db := newTestDB(t, behavior)
		repo := NewOrderRepository(db)

		created, err := repo.CreateWithItems(context.Background(), input)

		require.Error(t, err)
		require.ErrorContains(t, err, "create order item")
		require.ErrorIs(t, err, sql.ErrConnDone)
		require.Zero(t, created)
		require.Equal(t, 0, behavior.commitCount)
		require.Equal(t, 1, behavior.rollbackCount)
	})

	t.Run("commit failure", func(t *testing.T) {
		behavior := &testSQLBehavior{commitErr: sql.ErrTxDone}
		behavior.onQuery = func(query string, _ []driver.Value) (testQueryResult, error) {
			switch {
			case queryHasName(query, "CreateOrder"):
				return testQueryResult{
					columns: []string{"order_id", "user_id", "status", "currency", "total_amount", "created_at", "updated_at"},
					rows: [][]driver.Value{{
						orderID,
						userID,
						string(outbound.OrderStatusPending),
						"USD",
						int64(500),
						now,
						now,
					}},
				}, nil
			case queryHasName(query, "CreateOrderItem"):
				return testQueryResult{
					columns: []string{"order_item_id", "order_id", "product_id", "sku", "name", "quantity", "unit_price", "line_total", "currency", "created_at", "updated_at"},
					rows: [][]driver.Value{{
						uuid.New(),
						orderID,
						productID,
						"SKU-1",
						"Name",
						int64(1),
						int64(500),
						int64(500),
						"USD",
						now,
						now,
					}},
				}, nil
			case queryHasName(query, "CreateOrderSagaState"):
				return testQueryResult{
					columns: []string{"order_id", "stock_stage", "payment_stage", "last_error_code", "created_at", "updated_at"},
					rows: [][]driver.Value{{
						orderID,
						string(outbound.SagaStageNotStarted),
						string(outbound.SagaStageNotStarted),
						nil,
						now,
						now,
					}},
				}, nil
			default:
				return testQueryResult{}, fmt.Errorf("unexpected query: %s", query)
			}
		}
		behavior.onExec = func(query string, _ []driver.Value) (driver.Result, error) {
			if !queryHasName(query, "CreateOrderCheckoutIdempotency") {
				return nil, fmt.Errorf("unexpected exec: %s", query)
			}

			return driver.RowsAffected(1), nil
		}

		db := newTestDB(t, behavior)
		repo := NewOrderRepository(db)

		created, err := repo.CreateWithItems(context.Background(), input)

		require.Error(t, err)
		require.ErrorContains(t, err, "commit create order transaction")
		require.ErrorIs(t, err, sql.ErrTxDone)
		require.Zero(t, created)
		require.Equal(t, 1, behavior.commitCount)
		require.Equal(t, 0, behavior.rollbackCount)
	})
}

func TestOrderRepositoryGetByID(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	orderID := uuid.New()
	userID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
		itemsLen  int
	}{
		{
			name: "success",
			stub: stubQuerier{
				order: orderQuerierStub{
					getOrderByIDFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.Order, error) {
						require.Equal(t, orderID, gotOrderID)

						return sqlc.Order{
							OrderID:     orderID,
							UserID:      userID,
							Status:      sqlc.OrderStatus(outbound.OrderStatusPending),
							Currency:    "USD",
							TotalAmount: 300,
							CreatedAt:   now,
							UpdatedAt:   now,
						}, nil
					},
				},
				itemsHistory: itemsHistoryQuerierStub{
					listOrderItemsByOrderIDFunc: func(_ context.Context, gotOrderID uuid.UUID) ([]sqlc.OrderItem, error) {
						require.Equal(t, orderID, gotOrderID)
						return []sqlc.OrderItem{{
							OrderItemID: uuid.New(),
							OrderID:     orderID,
							ProductID:   uuid.New(),
							Sku:         "SKU-1",
							Name:        "Name",
							Quantity:    1,
							UnitPrice:   300,
							LineTotal:   300,
							Currency:    "USD",
							CreatedAt:   now,
							UpdatedAt:   now,
						}}, nil
					},
				},
			},
			itemsLen: 1,
		},
		{
			name: "not found",
			stub: stubQuerier{
				order: orderQuerierStub{
					getOrderByIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Order, error) {
						return sqlc.Order{}, sql.ErrNoRows
					},
				},
			},
			errIs: outbound.ErrOrderNotFound,
		},
		{
			name: "query error",
			stub: stubQuerier{
				order: orderQuerierStub{
					getOrderByIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Order, error) {
						return sqlc.Order{}, sql.ErrConnDone
					},
				},
			},
			errPrefix: "get order by id",
		},
		{
			name: "list items error",
			stub: stubQuerier{
				order: orderQuerierStub{
					getOrderByIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Order, error) {
						return sqlc.Order{OrderID: orderID, UserID: userID}, nil
					},
				},
				itemsHistory: itemsHistoryQuerierStub{
					listOrderItemsByOrderIDFunc: func(_ context.Context, _ uuid.UUID) ([]sqlc.OrderItem, error) {
						return nil, sql.ErrConnDone
					},
				},
			},
			errPrefix: "list order items by order id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewOrderRepositoryFromTransactionalQuerier(tt.stub)

			order, err := repo.GetByID(context.Background(), orderID)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, order)
				return
			}

			require.NoError(t, err)
			require.Equal(t, orderID, order.OrderID)
			require.Len(t, order.Items, tt.itemsLen)
		})
	}
}

func TestOrderRepositoryAppendStatusHistory(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	orderID := uuid.New()
	from := outbound.OrderStatusPending
	reason := "stock_reserved"

	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		itemsHistory: itemsHistoryQuerierStub{
			appendOrderStatusHistoryFunc: func(_ context.Context, arg sqlc.AppendOrderStatusHistoryParams) (sqlc.OrderStatusHistory, error) {
				require.Equal(t, orderID, arg.OrderID)
				require.Equal(t, string(from), arg.FromStatus.String)
				require.True(t, arg.FromStatus.Valid)
				require.Equal(t, string(outbound.OrderStatusAwaitingStock), arg.ToStatus)
				require.Equal(t, reason, arg.ReasonCode.String)
				require.True(t, arg.ReasonCode.Valid)

				return sqlc.OrderStatusHistory{
					OrderStatusHistoryID: uuid.New(),
					OrderID:              orderID,
					FromStatus:           sql.NullString{String: string(from), Valid: true},
					ToStatus:             string(outbound.OrderStatusAwaitingStock),
					ReasonCode:           sql.NullString{String: reason, Valid: true},
					CreatedAt:            now,
				}, nil
			},
		},
	})

	history, err := repo.AppendStatusHistory(
		context.Background(),
		orderID,
		&from,
		outbound.OrderStatusAwaitingStock,
		&reason,
	)

	require.NoError(t, err)
	require.Equal(t, orderID, history.OrderID)
	require.NotNil(t, history.FromStatus)
	require.Equal(t, from, *history.FromStatus)
	require.NotNil(t, history.ReasonCode)
	require.Equal(t, reason, *history.ReasonCode)
	require.Equal(t, outbound.OrderStatusAwaitingStock, history.ToStatus)
}

func TestOrderRepositoryTransitionStatusInvalidCurrentStatus(t *testing.T) {
	orderID := uuid.New()

	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		order: orderQuerierStub{
			getOrderByIDFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.Order, error) {
				require.Equal(t, orderID, gotOrderID)
				return sqlc.Order{OrderID: orderID}, nil
			},
			transitionOrderStatusFunc: func(_ context.Context, _ sqlc.TransitionOrderStatusParams) (sqlc.Order, error) {
				return sqlc.Order{}, sql.ErrNoRows
			},
		},
	})

	updated, err := repo.TransitionStatus(
		context.Background(),
		orderID,
		outbound.OrderStatusPending,
		outbound.OrderStatusAwaitingStock,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderInvalidStatusTransition)
	require.Zero(t, updated)
}

func TestOrderRepositoryTransitionStatusOrderNotFound(t *testing.T) {
	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		order: orderQuerierStub{
			getOrderByIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Order, error) {
				return sqlc.Order{}, sql.ErrNoRows
			},
			transitionOrderStatusFunc: func(_ context.Context, _ sqlc.TransitionOrderStatusParams) (sqlc.Order, error) {
				return sqlc.Order{}, sql.ErrNoRows
			},
		},
	})

	updated, err := repo.TransitionStatus(
		context.Background(),
		uuid.New(),
		outbound.OrderStatusPending,
		outbound.OrderStatusAwaitingStock,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderNotFound)
	require.Zero(t, updated)
}

func TestOrderRepositoryTransitionStatusCheckExistsError(t *testing.T) {
	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		order: orderQuerierStub{
			getOrderByIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Order, error) {
				return sqlc.Order{}, sql.ErrConnDone
			},
			transitionOrderStatusFunc: func(_ context.Context, _ sqlc.TransitionOrderStatusParams) (sqlc.Order, error) {
				return sqlc.Order{}, sql.ErrNoRows
			},
		},
	})

	updated, err := repo.TransitionStatus(
		context.Background(),
		uuid.New(),
		outbound.OrderStatusPending,
		outbound.OrderStatusAwaitingStock,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "check order exists before transition")
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.Zero(t, updated)
}

func TestOrderRepositoryTransitionStatusSuccess(t *testing.T) {
	orderID := uuid.New()

	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		order: orderQuerierStub{
			transitionOrderStatusFunc: func(_ context.Context, arg sqlc.TransitionOrderStatusParams) (sqlc.Order, error) {
				require.Equal(t, orderID, arg.OrderID)
				require.Equal(t, sqlc.OrderStatus(outbound.OrderStatusPending), arg.FromStatus)
				require.Equal(t, sqlc.OrderStatus(outbound.OrderStatusAwaitingStock), arg.ToStatus)

				return sqlc.Order{OrderID: orderID, Status: sqlc.OrderStatus(outbound.OrderStatusAwaitingStock)}, nil
			},
		},
	})

	updated, err := repo.TransitionStatus(
		context.Background(),
		orderID,
		outbound.OrderStatusPending,
		outbound.OrderStatusAwaitingStock,
	)

	require.NoError(t, err)
	require.Equal(t, orderID, updated.OrderID)
	require.Equal(t, outbound.OrderStatusAwaitingStock, updated.Status)
}

func TestOrderRepositoryAppendStatusHistoryNullMapping(t *testing.T) {
	orderID := uuid.New()

	repo := NewOrderRepositoryFromTransactionalQuerier(stubQuerier{
		itemsHistory: itemsHistoryQuerierStub{
			appendOrderStatusHistoryFunc: func(_ context.Context, arg sqlc.AppendOrderStatusHistoryParams) (sqlc.OrderStatusHistory, error) {
				require.False(t, arg.FromStatus.Valid)
				require.False(t, arg.ReasonCode.Valid)

				return sqlc.OrderStatusHistory{
					OrderStatusHistoryID: uuid.New(),
					OrderID:              orderID,
					ToStatus:             string(outbound.OrderStatusPending),
				}, nil
			},
		},
	})

	history, err := repo.AppendStatusHistory(context.Background(), orderID, nil, outbound.OrderStatusPending, nil)

	require.NoError(t, err)
	require.Nil(t, history.FromStatus)
	require.Nil(t, history.ReasonCode)
	require.Equal(t, outbound.OrderStatusPending, history.ToStatus)
}

func TestMapOrderWriteErrForeignKeyMapping(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		wantErr    error
	}{
		{name: "order items fk maps to not found", constraint: "order_items_order_id_fkey", wantErr: outbound.ErrOrderNotFound},
		{name: "saga state fk maps to not found", constraint: "order_saga_state_order_id_fkey", wantErr: outbound.ErrOrderNotFound},
		{name: "idempotency fk maps to not found", constraint: "order_checkout_idempotency_order_id_fkey", wantErr: outbound.ErrOrderNotFound},
		{name: "status history fk maps to not found", constraint: "order_status_history_order_id_fkey", wantErr: outbound.ErrOrderNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapOrderWriteErr(&pgconn.PgError{Code: "23503", ConstraintName: tt.constraint})
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestMapOrderWriteErrUnknownForeignKeyPassthrough(t *testing.T) {
	original := &pgconn.PgError{Code: "23503", ConstraintName: "some_other_fk"}
	mapped := mapOrderWriteErr(original)
	require.ErrorIs(t, mapped, original)
}

package repos

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

func TestOrderSagaStateRepositoryTransitionRequested(t *testing.T) {
	t.Run("stock stage requested success", func(t *testing.T) {
		now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
		orderID := uuid.New()

		repo := &OrderSagaStateRepository{queries: stubQuerier{
			saga: sagaQuerierStub{
				markOrderSagaStockRequestedFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
					require.Equal(t, orderID, gotOrderID)

					return sqlc.OrderSagaState{
						OrderID:      orderID,
						StockStage:   sqlc.OrderSagaStageRequested,
						PaymentStage: sqlc.OrderSagaStageNotStarted,
						CreatedAt:    now,
						UpdatedAt:    now,
					}, nil
				},
			},
		}}

		state, err := repo.TransitionStockStageToRequested(context.Background(), orderID)

		require.NoError(t, err)
		require.Equal(t, orderID, state.OrderID)
		require.Equal(t, outbound.SagaStageRequested, state.StockStage)
		require.Equal(t, outbound.SagaStageNotStarted, state.PaymentStage)
	})

	t.Run("payment stage requested success", func(t *testing.T) {
		now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
		orderID := uuid.New()

		repo := &OrderSagaStateRepository{queries: stubQuerier{
			saga: sagaQuerierStub{
				markOrderSagaPaymentRequestedFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
					require.Equal(t, orderID, gotOrderID)

					return sqlc.OrderSagaState{
						OrderID:      orderID,
						StockStage:   sqlc.OrderSagaStageNotStarted,
						PaymentStage: sqlc.OrderSagaStageRequested,
						CreatedAt:    now,
						UpdatedAt:    now,
					}, nil
				},
			},
		}}

		state, err := repo.TransitionPaymentStageToRequested(context.Background(), orderID)

		require.NoError(t, err)
		require.Equal(t, orderID, state.OrderID)
		require.Equal(t, outbound.SagaStageNotStarted, state.StockStage)
		require.Equal(t, outbound.SagaStageRequested, state.PaymentStage)
	})
}

func TestOrderSagaStateRepositoryTransitionTerminal(t *testing.T) {
	tests := []struct {
		name                 string
		call                 func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error)
		stub                 func(orderID uuid.UUID, now time.Time) stubQuerier
		wantStockStage       outbound.SagaStage
		wantPaymentStage     outbound.SagaStage
		targetedStageLabel   string
		untargetedStageLabel string
	}{
		{
			name: "stock succeeded",
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionStockStageToSucceeded(ctx, orderID)
			},
			stub: func(orderID uuid.UUID, now time.Time) stubQuerier {
				return stubQuerier{
					saga: sagaQuerierStub{
						markOrderSagaStockSucceededFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
							require.Equal(t, orderID, gotOrderID)

							return sqlc.OrderSagaState{
								OrderID:      orderID,
								StockStage:   sqlc.OrderSagaStageSucceeded,
								PaymentStage: sqlc.OrderSagaStageRequested,
								CreatedAt:    now,
								UpdatedAt:    now,
							}, nil
						},
					},
				}
			},
			wantStockStage:       outbound.SagaStageSucceeded,
			wantPaymentStage:     outbound.SagaStageRequested,
			targetedStageLabel:   "stock",
			untargetedStageLabel: "payment",
		},
		{
			name: "stock failed",
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionStockStageToFailed(ctx, orderID)
			},
			stub: func(orderID uuid.UUID, now time.Time) stubQuerier {
				return stubQuerier{
					saga: sagaQuerierStub{
						markOrderSagaStockFailedFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
							require.Equal(t, orderID, gotOrderID)

							return sqlc.OrderSagaState{
								OrderID:      orderID,
								StockStage:   sqlc.OrderSagaStageFailed,
								PaymentStage: sqlc.OrderSagaStageRequested,
								CreatedAt:    now,
								UpdatedAt:    now,
							}, nil
						},
					},
				}
			},
			wantStockStage:       outbound.SagaStageFailed,
			wantPaymentStage:     outbound.SagaStageRequested,
			targetedStageLabel:   "stock",
			untargetedStageLabel: "payment",
		},
		{
			name: "payment succeeded",
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToSucceeded(ctx, orderID)
			},
			stub: func(orderID uuid.UUID, now time.Time) stubQuerier {
				return stubQuerier{
					saga: sagaQuerierStub{
						markOrderSagaPaymentSucceededFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
							require.Equal(t, orderID, gotOrderID)

							return sqlc.OrderSagaState{
								OrderID:      orderID,
								StockStage:   sqlc.OrderSagaStageRequested,
								PaymentStage: sqlc.OrderSagaStageSucceeded,
								CreatedAt:    now,
								UpdatedAt:    now,
							}, nil
						},
					},
				}
			},
			wantStockStage:       outbound.SagaStageRequested,
			wantPaymentStage:     outbound.SagaStageSucceeded,
			targetedStageLabel:   "payment",
			untargetedStageLabel: "stock",
		},
		{
			name: "payment failed",
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToFailed(ctx, orderID)
			},
			stub: func(orderID uuid.UUID, now time.Time) stubQuerier {
				return stubQuerier{
					saga: sagaQuerierStub{
						markOrderSagaPaymentFailedFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
							require.Equal(t, orderID, gotOrderID)

							return sqlc.OrderSagaState{
								OrderID:      orderID,
								StockStage:   sqlc.OrderSagaStageRequested,
								PaymentStage: sqlc.OrderSagaStageFailed,
								CreatedAt:    now,
								UpdatedAt:    now,
							}, nil
						},
					},
				}
			},
			wantStockStage:       outbound.SagaStageRequested,
			wantPaymentStage:     outbound.SagaStageFailed,
			targetedStageLabel:   "payment",
			untargetedStageLabel: "stock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
			orderID := uuid.New()
			repo := &OrderSagaStateRepository{queries: tt.stub(orderID, now)}

			state, err := tt.call(repo, context.Background(), orderID)

			require.NoError(t, err)
			require.Equal(t, orderID, state.OrderID)
			require.Equalf(t, tt.wantStockStage, state.StockStage, "targeted=%s untargeted=%s", tt.targetedStageLabel, tt.untargetedStageLabel)
			require.Equalf(t, tt.wantPaymentStage, state.PaymentStage, "targeted=%s untargeted=%s", tt.targetedStageLabel, tt.untargetedStageLabel)
		})
	}
}

func TestOrderSagaStateRepositoryTransitionNoRowsMapping(t *testing.T) {
	tests := []struct {
		name string
		call func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error)
		stub func(orderID uuid.UUID) stubQuerier
		want error
	}{
		{
			name: "invalid transition when state exists",
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionStockStageToSucceeded(ctx, orderID)
			},
			stub: func(orderID uuid.UUID) stubQuerier {
				return stubQuerier{
					saga: sagaQuerierStub{
						markOrderSagaStockSucceededFunc: func(_ context.Context, _ uuid.UUID) (sqlc.OrderSagaState, error) {
							return sqlc.OrderSagaState{}, sql.ErrNoRows
						},
						getOrderSagaStateByOrderIDFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
							require.Equal(t, orderID, gotOrderID)
							return sqlc.OrderSagaState{OrderID: orderID}, nil
						},
					},
				}
			},
			want: outbound.ErrOrderSagaStateInvalidTransition,
		},
		{
			name: "not found when state missing",
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToFailed(ctx, orderID)
			},
			stub: func(orderID uuid.UUID) stubQuerier {
				return stubQuerier{
					saga: sagaQuerierStub{
						markOrderSagaPaymentFailedFunc: func(_ context.Context, _ uuid.UUID) (sqlc.OrderSagaState, error) {
							return sqlc.OrderSagaState{}, sql.ErrNoRows
						},
						getOrderSagaStateByOrderIDFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
							require.Equal(t, orderID, gotOrderID)
							return sqlc.OrderSagaState{}, sql.ErrNoRows
						},
					},
				}
			},
			want: outbound.ErrOrderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderID := uuid.New()
			repo := &OrderSagaStateRepository{queries: tt.stub(orderID)}

			state, err := tt.call(repo, context.Background(), orderID)

			require.Error(t, err)
			require.ErrorIs(t, err, tt.want)
			require.Zero(t, state)
		})
	}
}

func TestOrderSagaStateRepositorySetAndClearLastErrorCode(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	orderID := uuid.New()
	errCode := "payment_timeout"

	repo := &OrderSagaStateRepository{queries: stubQuerier{
		saga: sagaQuerierStub{
			setOrderSagaLastErrorCodeFunc: func(_ context.Context, arg sqlc.SetOrderSagaLastErrorCodeParams) (sqlc.OrderSagaState, error) {
				require.Equal(t, orderID, arg.OrderID)
				require.True(t, arg.LastErrorCode.Valid)
				require.Equal(t, errCode, arg.LastErrorCode.String)

				return sqlc.OrderSagaState{
					OrderID:       orderID,
					StockStage:    sqlc.OrderSagaStageRequested,
					PaymentStage:  sqlc.OrderSagaStageRequested,
					LastErrorCode: sql.NullString{String: errCode, Valid: true},
					CreatedAt:     now,
					UpdatedAt:     now,
				}, nil
			},
			clearOrderSagaLastErrorCodeFunc: func(_ context.Context, gotOrderID uuid.UUID) (sqlc.OrderSagaState, error) {
				require.Equal(t, orderID, gotOrderID)

				return sqlc.OrderSagaState{
					OrderID:      orderID,
					StockStage:   sqlc.OrderSagaStageRequested,
					PaymentStage: sqlc.OrderSagaStageRequested,
					CreatedAt:    now,
					UpdatedAt:    now,
				}, nil
			},
		},
	}}

	setState, err := repo.SetLastErrorCode(context.Background(), orderID, errCode)
	require.NoError(t, err)
	require.NotNil(t, setState.LastErrorCode)
	require.Equal(t, errCode, *setState.LastErrorCode)

	clearedState, err := repo.ClearLastErrorCode(context.Background(), orderID)
	require.NoError(t, err)
	require.Nil(t, clearedState.LastErrorCode)
}

func TestOrderSagaStateRepositoryLastErrorCodeNotFound(t *testing.T) {
	orderID := uuid.New()

	repo := &OrderSagaStateRepository{queries: stubQuerier{
		saga: sagaQuerierStub{
			setOrderSagaLastErrorCodeFunc: func(_ context.Context, _ sqlc.SetOrderSagaLastErrorCodeParams) (sqlc.OrderSagaState, error) {
				return sqlc.OrderSagaState{}, sql.ErrNoRows
			},
			clearOrderSagaLastErrorCodeFunc: func(_ context.Context, _ uuid.UUID) (sqlc.OrderSagaState, error) {
				return sqlc.OrderSagaState{}, sql.ErrNoRows
			},
		},
	}}

	_, err := repo.SetLastErrorCode(context.Background(), orderID, "x")
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderNotFound)

	_, err = repo.ClearLastErrorCode(context.Background(), orderID)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderNotFound)
}

func TestOrderSagaStateRepositoryTransitionSemanticsPostgres(t *testing.T) {
	db := openPostgresTestDB(t)

	tests := []struct {
		name         string
		initialStock outbound.SagaStage
		initialPay   outbound.SagaStage
		call         func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error)
		wantErr      error
	}{
		{
			name:         "stock terminal flip forbidden",
			initialStock: outbound.SagaStageSucceeded,
			initialPay:   outbound.SagaStageRequested,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionStockStageToFailed(ctx, orderID)
			},
			wantErr: outbound.ErrOrderSagaStateInvalidTransition,
		},
		{
			name:         "stock reopen forbidden",
			initialStock: outbound.SagaStageSucceeded,
			initialPay:   outbound.SagaStageRequested,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionStockStageToRequested(ctx, orderID)
			},
			wantErr: outbound.ErrOrderSagaStateInvalidTransition,
		},
		{
			name:         "payment terminal flip forbidden",
			initialStock: outbound.SagaStageRequested,
			initialPay:   outbound.SagaStageFailed,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToSucceeded(ctx, orderID)
			},
			wantErr: outbound.ErrOrderSagaStateInvalidTransition,
		},
		{
			name:         "payment requested requires stock succeeded",
			initialStock: outbound.SagaStageRequested,
			initialPay:   outbound.SagaStageNotStarted,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToRequested(ctx, orderID)
			},
			wantErr: outbound.ErrOrderSagaStateInvalidTransition,
		},
		{
			name:         "payment reopen forbidden",
			initialStock: outbound.SagaStageRequested,
			initialPay:   outbound.SagaStageSucceeded,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToRequested(ctx, orderID)
			},
			wantErr: outbound.ErrOrderSagaStateInvalidTransition,
		},
		{
			name:         "missing state reports not found",
			initialStock: outbound.SagaStageNotStarted,
			initialPay:   outbound.SagaStageNotStarted,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToFailed(ctx, orderID)
			},
			wantErr: outbound.ErrOrderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tx := beginMigratedOrderSagaTx(t, db)
			repo := NewOrderSagaStateRepositoryFromTx(tx)

			orderID := uuid.New()
			if tt.name != "missing state reports not found" {
				seedOrderAndSagaState(t, ctx, tx, orderID, tt.initialStock, tt.initialPay)
			}

			state, err := tt.call(repo, ctx, orderID)

			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
			require.Zero(t, state)
		})
	}
}

func TestOrderSagaStateRepositoryTransitionIdempotentPostgres(t *testing.T) {
	db := openPostgresTestDB(t)

	tests := []struct {
		name              string
		initialStock      outbound.SagaStage
		initialPay        outbound.SagaStage
		call              func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error)
		wantStockStage    outbound.SagaStage
		wantPaymentStage  outbound.SagaStage
		targetedStageName string
	}{
		{
			name:         "stock requested idempotent",
			initialStock: outbound.SagaStageRequested,
			initialPay:   outbound.SagaStageNotStarted,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionStockStageToRequested(ctx, orderID)
			},
			wantStockStage:    outbound.SagaStageRequested,
			wantPaymentStage:  outbound.SagaStageNotStarted,
			targetedStageName: "stock",
		},
		{
			name:         "stock succeeded idempotent",
			initialStock: outbound.SagaStageSucceeded,
			initialPay:   outbound.SagaStageRequested,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionStockStageToSucceeded(ctx, orderID)
			},
			wantStockStage:    outbound.SagaStageSucceeded,
			wantPaymentStage:  outbound.SagaStageRequested,
			targetedStageName: "stock",
		},
		{
			name:         "payment failed idempotent",
			initialStock: outbound.SagaStageRequested,
			initialPay:   outbound.SagaStageFailed,
			call: func(repo *OrderSagaStateRepository, ctx context.Context, orderID uuid.UUID) (outbound.SagaState, error) {
				return repo.TransitionPaymentStageToFailed(ctx, orderID)
			},
			wantStockStage:    outbound.SagaStageRequested,
			wantPaymentStage:  outbound.SagaStageFailed,
			targetedStageName: "payment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tx := beginMigratedOrderSagaTx(t, db)
			repo := NewOrderSagaStateRepositoryFromTx(tx)

			orderID := uuid.New()
			seedOrderAndSagaState(t, ctx, tx, orderID, tt.initialStock, tt.initialPay)

			state, err := tt.call(repo, ctx, orderID)

			require.NoError(t, err)
			require.Equalf(t, tt.wantStockStage, state.StockStage, "targeted stage=%s", tt.targetedStageName)
			require.Equalf(t, tt.wantPaymentStage, state.PaymentStage, "targeted stage=%s", tt.targetedStageName)

			persisted := getSagaStateRow(t, ctx, tx, orderID)
			require.Equal(t, tt.wantStockStage, persisted.StockStage)
			require.Equal(t, tt.wantPaymentStage, persisted.PaymentStage)
		})
	}
}

func TestCreateOrderSagaStateMigrationUpDownSafety(t *testing.T) {
	db := openPostgresTestDB(t)
	ctx := context.Background()

	t.Run("up creates enum columns and preserves defaults", func(t *testing.T) {
		tx := beginBaseOrderSagaTx(t, db)

		assertSagaStageColumnType(t, ctx, tx, "stock_stage", "order_saga_stage")
		assertSagaStageColumnType(t, ctx, tx, "payment_stage", "order_saga_stage")

		var stockDefault, paymentDefault string
		err := tx.QueryRowContext(ctx, `
			SELECT column_default
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'order_saga_state'
			  AND column_name = 'stock_stage'
		`).Scan(&stockDefault)
		require.NoError(t, err)

		err = tx.QueryRowContext(ctx, `
			SELECT column_default
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'order_saga_state'
			  AND column_name = 'payment_stage'
		`).Scan(&paymentDefault)
		require.NoError(t, err)

		require.Contains(t, stockDefault, "not_started")
		require.Contains(t, paymentDefault, "not_started")

		defaultOrderID := uuid.New()
		insertOrder(t, ctx, tx, defaultOrderID)
		_, err = tx.ExecContext(ctx, `INSERT INTO order_saga_state(order_id) VALUES ($1)`, defaultOrderID)
		require.NoError(t, err)

		var stock, payment string
		err = tx.QueryRowContext(ctx, `
			SELECT stock_stage::text, payment_stage::text
			FROM order_saga_state
			WHERE order_id = $1
		`, defaultOrderID).Scan(&stock, &payment)
		require.NoError(t, err)
		require.Equal(t, string(outbound.SagaStageNotStarted), stock)
		require.Equal(t, string(outbound.SagaStageNotStarted), payment)

		invalidOrderID := uuid.New()
		insertOrder(t, ctx, tx, invalidOrderID)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_saga_state(order_id, stock_stage, payment_stage)
			VALUES ($1, 'invalid', 'not_started')
		`, invalidOrderID)
		require.Error(t, err)
	})

	t.Run("down drops table and enum type", func(t *testing.T) {
		tx := beginBaseOrderSagaTx(t, db)

		require.NoError(t, execMigrationSection(ctx, tx, migrationCreateOrderSagaState, "Down"))

		var tableExists bool
		err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.tables
				WHERE table_schema = current_schema()
				  AND table_name = 'order_saga_state'
			)
		`).Scan(&tableExists)
		require.NoError(t, err)
		require.False(t, tableExists)

		var typeExists bool
		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_type t
				JOIN pg_namespace n ON n.oid = t.typnamespace
				WHERE n.nspname = current_schema()
				  AND t.typname = 'order_saga_stage'
			)
		`).Scan(&typeExists)
		require.NoError(t, err)
		require.False(t, typeExists)
	})
}

func openPostgresTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("ORDER_SVC_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("skip postgres-backed test: ORDER_SVC_TEST_POSTGRES_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("skip postgres-backed test: open db: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("skip postgres-backed test: ping db: %v", err)
	}

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func beginBaseOrderSagaTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	cleanup := func() {
		require.NoError(t, tx.Rollback())
	}

	t.Cleanup(cleanup)

	schema := "order_svc_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `SET LOCAL search_path TO `+schema)
	require.NoError(t, err)

	require.NoError(t, execMigrationSection(ctx, tx, migrationCreateOrders, "Up"))
	require.NoError(t, execMigrationSection(ctx, tx, migrationCreateOrderSagaState, "Up"))

	return tx
}

func beginMigratedOrderSagaTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()

	return beginBaseOrderSagaTx(t, db)
}

func seedOrderAndSagaState(t *testing.T, ctx context.Context, tx *sql.Tx, orderID uuid.UUID, stock, payment outbound.SagaStage) {
	t.Helper()

	insertOrder(t, ctx, tx, orderID)

	_, err := tx.ExecContext(ctx, `
		INSERT INTO order_saga_state(order_id, stock_stage, payment_stage)
		VALUES ($1, $2, $3)
	`, orderID, string(stock), string(payment))
	require.NoError(t, err)
}

func insertOrder(t *testing.T, ctx context.Context, tx *sql.Tx, orderID uuid.UUID) {
	t.Helper()

	_, err := tx.ExecContext(ctx, `
		INSERT INTO orders(order_id, user_id, status, currency, total_amount)
		VALUES ($1, $2, 'pending', 'USD', 100)
	`, orderID, uuid.New())
	require.NoError(t, err)
}

func getSagaStateRow(t *testing.T, ctx context.Context, tx *sql.Tx, orderID uuid.UUID) outbound.SagaState {
	t.Helper()

	var stock, payment string
	err := tx.QueryRowContext(ctx, `
		SELECT stock_stage::text, payment_stage::text
		FROM order_saga_state
		WHERE order_id = $1
	`, orderID).Scan(&stock, &payment)
	require.NoError(t, err)

	return outbound.SagaState{
		OrderID:      orderID,
		StockStage:   outbound.SagaStage(stock),
		PaymentStage: outbound.SagaStage(payment),
	}
}

func assertSagaStageColumnType(t *testing.T, ctx context.Context, tx *sql.Tx, columnName, wantType string) {
	t.Helper()

	var gotType string
	err := tx.QueryRowContext(ctx, `
		SELECT CASE
			WHEN data_type = 'USER-DEFINED' THEN udt_name
			ELSE data_type
		END
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'order_saga_state'
		  AND column_name = $1
	`, columnName).Scan(&gotType)
	require.NoError(t, err)
	require.Equal(t, wantType, gotType)
}

func execMigrationSection(ctx context.Context, tx *sql.Tx, filename, section string) error {
	raw, err := os.ReadFile(filepath.Join("..", "migrations", filename))
	if err != nil {
		return fmt.Errorf("read migration %s: %w", filename, err)
	}

	sectionSQL, err := extractGooseSection(string(raw), section)
	if err != nil {
		return fmt.Errorf("extract %s from %s: %w", section, filename, err)
	}

	if strings.TrimSpace(sectionSQL) == "" {
		return nil
	}

	if _, err := tx.ExecContext(ctx, sectionSQL); err != nil {
		return fmt.Errorf("exec %s from %s: %w", section, filename, err)
	}

	return nil
}

func extractGooseSection(raw string, section string) (string, error) {
	marker := "-- +goose " + section
	start := strings.Index(raw, marker)
	if start < 0 {
		return "", fmt.Errorf("marker %q not found", marker)
	}

	content := raw[start+len(marker):]
	if section == "Up" {
		if downIdx := strings.Index(content, "-- +goose Down"); downIdx >= 0 {
			content = content[:downIdx]
		}
	}

	lines := strings.Split(content, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-- +goose ") {
			continue
		}

		filtered = append(filtered, line)
	}

	return strings.TrimSpace(strings.Join(filtered, "\n")), nil
}

const (
	migrationCreateOrders         = "20260417190741852_create_orders.sql"
	migrationCreateOrderSagaState = "20260417192522491_create_order_saga_state.sql"
)

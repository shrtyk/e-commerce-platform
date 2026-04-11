package repos

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
)

func TestStockRepositoryCreate(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	productID := uuid.New()

	tests := []struct {
		name      string
		input     domain.StockRecord
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name:  "success",
			input: domain.StockRecord{ProductID: productID, Quantity: 10, Reserved: 2},
			stub: stubQuerier{
				createStockFunc: func(_ context.Context, arg sqlc.CreateStockRecordParams) (sqlc.StockRecord, error) {
					require.Equal(t, productID, arg.ProductID)
					require.Equal(t, int32(10), arg.Quantity)
					require.Equal(t, int32(2), arg.Reserved)

					return sqlc.StockRecord{
						StockRecordID: uuid.New(),
						ProductID:     arg.ProductID,
						Quantity:      arg.Quantity,
						Reserved:      arg.Reserved,
						Available:     sql.NullInt32{Int32: 8, Valid: true},
						CreatedAt:     now,
						UpdatedAt:     now,
					}, nil
				},
			},
		},
		{
			name:  "invalid stock update",
			input: domain.StockRecord{ProductID: productID, Quantity: 1, Reserved: 2},
			stub: stubQuerier{
				createStockFunc: func(_ context.Context, _ sqlc.CreateStockRecordParams) (sqlc.StockRecord, error) {
					return sqlc.StockRecord{}, &pgconn.PgError{Code: "23514"}
				},
			},
			errIs:     outbound.ErrInvalidStockUpdate,
			errPrefix: "create stock record",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &StockRepository{queries: tt.stub}

			stock, err := repo.Create(context.Background(), tt.input)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, stock)
				return
			}

			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, stock.StockRecordID)
			require.Equal(t, int32(8), stock.Available)
			require.Equal(t, domain.StockRecordStatusInStock, stock.Status)
		})
	}
}

func TestStockRepositoryGetByProductID(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	productID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				getStockFunc: func(_ context.Context, gotProductID uuid.UUID) (sqlc.StockRecord, error) {
					require.Equal(t, productID, gotProductID)

					return sqlc.StockRecord{
						StockRecordID: uuid.New(),
						ProductID:     productID,
						Quantity:      5,
						Reserved:      5,
						Available:     sql.NullInt32{Int32: 0, Valid: true},
						CreatedAt:     now,
						UpdatedAt:     now,
					}, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				getStockFunc: func(_ context.Context, _ uuid.UUID) (sqlc.StockRecord, error) {
					return sqlc.StockRecord{}, sql.ErrNoRows
				},
			},
			errIs: outbound.ErrStockRecordNotFound,
		},
		{
			name: "db error",
			stub: stubQuerier{
				getStockFunc: func(_ context.Context, _ uuid.UUID) (sqlc.StockRecord, error) {
					return sqlc.StockRecord{}, sql.ErrConnDone
				},
			},
			errPrefix: "get stock record by product id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &StockRepository{queries: tt.stub}

			stock, err := repo.GetByProductID(context.Background(), productID)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, stock)
				return
			}

			require.NoError(t, err)
			require.Equal(t, int32(0), stock.Available)
			require.Equal(t, domain.StockRecordStatusOutOfStock, stock.Status)
		})
	}
}

func TestStockRepositoryUpdate(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	stockRecordID := uuid.New()
	productID := uuid.New()

	tests := []struct {
		name      string
		input     domain.StockRecord
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name:  "success",
			input: domain.StockRecord{StockRecordID: stockRecordID, Quantity: 10, Reserved: 3},
			stub: stubQuerier{
				updateStockFunc: func(_ context.Context, arg sqlc.UpdateStockRecordParams) (sqlc.StockRecord, error) {
					require.Equal(t, stockRecordID, arg.StockRecordID)
					require.Equal(t, int32(10), arg.Quantity)
					require.Equal(t, int32(3), arg.Reserved)

					return sqlc.StockRecord{
						StockRecordID: stockRecordID,
						ProductID:     productID,
						Quantity:      arg.Quantity,
						Reserved:      arg.Reserved,
						Available:     sql.NullInt32{Int32: 7, Valid: true},
						CreatedAt:     now,
						UpdatedAt:     now,
					}, nil
				},
			},
		},
		{
			name:  "not found",
			input: domain.StockRecord{StockRecordID: stockRecordID, Quantity: 10, Reserved: 1},
			stub: stubQuerier{
				updateStockFunc: func(_ context.Context, _ sqlc.UpdateStockRecordParams) (sqlc.StockRecord, error) {
					return sqlc.StockRecord{}, sql.ErrNoRows
				},
			},
			errIs: outbound.ErrStockRecordNotFound,
		},
		{
			name:  "invalid update",
			input: domain.StockRecord{StockRecordID: stockRecordID, Quantity: 1, Reserved: 2},
			stub: stubQuerier{
				updateStockFunc: func(_ context.Context, _ sqlc.UpdateStockRecordParams) (sqlc.StockRecord, error) {
					return sqlc.StockRecord{}, &pgconn.PgError{Code: "23514"}
				},
			},
			errIs:     outbound.ErrInvalidStockUpdate,
			errPrefix: "update stock record",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &StockRepository{queries: tt.stub}

			stock, err := repo.Update(context.Background(), tt.input)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, stock)
				return
			}

			require.NoError(t, err)
			require.Equal(t, int32(7), stock.Available)
			require.Equal(t, domain.StockRecordStatusInStock, stock.Status)
		})
	}
}

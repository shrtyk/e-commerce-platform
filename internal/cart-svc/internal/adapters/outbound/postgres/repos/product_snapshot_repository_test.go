package repos

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/sqlc"
)

func TestProductSnapshotRepositoryGetBySKU(t *testing.T) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	sku := "SKU-1"
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
				getProductBySKUFunc: func(_ context.Context, gotSKU string) (sqlc.ProductSnapshot, error) {
					require.Equal(t, sku, gotSKU)
					return sqlc.ProductSnapshot{
						Sku:       sku,
						ProductID: uuid.NullUUID{UUID: productID, Valid: true},
						Name:      "Product",
						UnitPrice: 1000,
						Currency:  "USD",
						CreatedAt: now,
						UpdatedAt: now,
					}, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				getProductBySKUFunc: func(_ context.Context, _ string) (sqlc.ProductSnapshot, error) {
					return sqlc.ProductSnapshot{}, sql.ErrNoRows
				},
			},
			errIs: ErrProductSnapshotNotFound,
		},
		{
			name: "query error",
			stub: stubQuerier{
				getProductBySKUFunc: func(_ context.Context, _ string) (sqlc.ProductSnapshot, error) {
					return sqlc.ProductSnapshot{}, sql.ErrConnDone
				},
			},
			errPrefix: "get product snapshot by sku",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductSnapshotRepository{queries: tt.stub}
			snapshot, err := repo.GetBySKU(context.Background(), sku)

			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, snapshot)
				return
			}

			require.NoError(t, err)
			require.Equal(t, sku, snapshot.Sku)
		})
	}
}

func TestProductSnapshotRepositoryUpsert(t *testing.T) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	productID := uuid.New()

	params := sqlc.UpsertProductSnapshotParams{
		Sku:       "SKU-1",
		ProductID: uuid.NullUUID{UUID: productID, Valid: true},
		Name:      "Product",
		UnitPrice: 1000,
		Currency:  "USD",
	}

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				upsertProductSnapshotFunc: func(_ context.Context, got sqlc.UpsertProductSnapshotParams) (sqlc.ProductSnapshot, error) {
					require.Equal(t, params, got)
					return sqlc.ProductSnapshot{
						Sku:       got.Sku,
						ProductID: got.ProductID,
						Name:      got.Name,
						UnitPrice: got.UnitPrice,
						Currency:  got.Currency,
						CreatedAt: now,
						UpdatedAt: now,
					}, nil
				},
			},
		},
		{
			name: "conflict product id",
			stub: stubQuerier{
				upsertProductSnapshotFunc: func(_ context.Context, _ sqlc.UpsertProductSnapshotParams) (sqlc.ProductSnapshot, error) {
					return sqlc.ProductSnapshot{}, &pgconn.PgError{Code: "23505", ConstraintName: "uq_product_snapshots_product_id"}
				},
			},
			errIs:     ErrProductSnapshotConflict,
			errPrefix: "upsert product snapshot",
		},
		{
			name: "query error",
			stub: stubQuerier{
				upsertProductSnapshotFunc: func(_ context.Context, _ sqlc.UpsertProductSnapshotParams) (sqlc.ProductSnapshot, error) {
					return sqlc.ProductSnapshot{}, sql.ErrConnDone
				},
			},
			errPrefix: "upsert product snapshot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductSnapshotRepository{queries: tt.stub}
			snapshot, err := repo.Upsert(context.Background(), params)

			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, snapshot)
				return
			}

			require.NoError(t, err)
			require.Equal(t, params.Sku, snapshot.Sku)
		})
	}
}

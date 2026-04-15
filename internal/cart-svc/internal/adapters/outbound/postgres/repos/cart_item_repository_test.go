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

func TestCartItemRepositoryListByCartID(t *testing.T) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	cartID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errPrefix string
		count     int
	}{
		{
			name: "success",
			stub: stubQuerier{
				listCartItemsByCartIDFunc: func(_ context.Context, gotCartID uuid.UUID) ([]sqlc.CartItem, error) {
					require.Equal(t, cartID, gotCartID)
					return []sqlc.CartItem{
						{
							CartItemID:  uuid.New(),
							CartID:      cartID,
							Sku:         "SKU-1",
							Quantity:    1,
							UnitPrice:   1000,
							Currency:    "USD",
							ProductName: "One",
							CreatedAt:   now,
							UpdatedAt:   now,
						},
						{
							CartItemID:  uuid.New(),
							CartID:      cartID,
							Sku:         "SKU-2",
							Quantity:    2,
							UnitPrice:   2000,
							Currency:    "USD",
							ProductName: "Two",
							CreatedAt:   now,
							UpdatedAt:   now,
						},
					}, nil
				},
			},
			count: 2,
		},
		{
			name: "query error",
			stub: stubQuerier{
				listCartItemsByCartIDFunc: func(_ context.Context, _ uuid.UUID) ([]sqlc.CartItem, error) {
					return nil, sql.ErrConnDone
				},
			},
			errPrefix: "list cart items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &CartItemRepository{queries: tt.stub}

			items, err := repo.ListByCartID(context.Background(), cartID)
			if tt.errPrefix != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errPrefix)
				require.Nil(t, items)
				return
			}

			require.NoError(t, err)
			require.Len(t, items, tt.count)
		})
	}
}

func TestCartItemRepositoryInsert(t *testing.T) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	cartID := uuid.New()

	item := sqlc.InsertCartItemParams{
		CartID:      cartID,
		Sku:         "SKU-1",
		Quantity:    2,
		UnitPrice:   1500,
		Currency:    "USD",
		ProductName: "Name",
	}

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
		quantity  int32
	}{
		{
			name: "success",
			stub: stubQuerier{
				insertCartItemFunc: func(_ context.Context, got sqlc.InsertCartItemParams) (sqlc.CartItem, error) {
					require.Equal(t, item, got)
					return sqlc.CartItem{
						CartItemID:  uuid.New(),
						CartID:      got.CartID,
						Sku:         got.Sku,
						Quantity:    got.Quantity,
						UnitPrice:   got.UnitPrice,
						Currency:    got.Currency,
						ProductName: got.ProductName,
						CreatedAt:   now,
						UpdatedAt:   now,
					}, nil
				},
			},
			quantity: item.Quantity,
		},
		{
			name: "duplicate add merged",
			stub: stubQuerier{
				insertCartItemFunc: func(_ context.Context, got sqlc.InsertCartItemParams) (sqlc.CartItem, error) {
					require.Equal(t, item, got)
					return sqlc.CartItem{
						CartItemID:  uuid.New(),
						CartID:      got.CartID,
						Sku:         got.Sku,
						Quantity:    got.Quantity + 3,
						UnitPrice:   got.UnitPrice,
						Currency:    got.Currency,
						ProductName: got.ProductName,
						CreatedAt:   now,
						UpdatedAt:   now,
					}, nil
				},
			},
			quantity: item.Quantity + 3,
		},
		{
			name: "missing cart fk",
			stub: stubQuerier{
				insertCartItemFunc: func(_ context.Context, _ sqlc.InsertCartItemParams) (sqlc.CartItem, error) {
					return sqlc.CartItem{}, &pgconn.PgError{Code: "23503", ConstraintName: "cart_items_cart_id_fkey"}
				},
			},
			errIs:     ErrCartNotFound,
			errPrefix: "insert cart item",
		},
		{
			name: "missing product snapshot fk",
			stub: stubQuerier{
				insertCartItemFunc: func(_ context.Context, _ sqlc.InsertCartItemParams) (sqlc.CartItem, error) {
					return sqlc.CartItem{}, &pgconn.PgError{Code: "23503", ConstraintName: "cart_items_sku_fkey"}
				},
			},
			errIs:     ErrProductSnapshotNotFound,
			errPrefix: "insert cart item",
		},
		{
			name: "db error",
			stub: stubQuerier{
				insertCartItemFunc: func(_ context.Context, _ sqlc.InsertCartItemParams) (sqlc.CartItem, error) {
					return sqlc.CartItem{}, sql.ErrConnDone
				},
			},
			errPrefix: "insert cart item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &CartItemRepository{queries: tt.stub}
			stored, err := repo.Insert(context.Background(), item)

			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, stored)
				return
			}

			require.NoError(t, err)
			require.Equal(t, item.Sku, stored.Sku)
			require.Equal(t, tt.quantity, stored.Quantity)
		})
	}
}

func TestCartItemRepositoryUpdateQuantity(t *testing.T) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	cartID := uuid.New()
	sku := "SKU-1"
	qty := int32(3)

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				updateCartItemQtyFunc: func(_ context.Context, arg sqlc.UpdateCartItemQuantityParams) (sqlc.CartItem, error) {
					require.Equal(t, cartID, arg.CartID)
					require.Equal(t, sku, arg.Sku)
					require.Equal(t, qty, arg.Quantity)
					return sqlc.CartItem{
						CartItemID:  uuid.New(),
						CartID:      cartID,
						Sku:         sku,
						Quantity:    qty,
						UnitPrice:   100,
						Currency:    "USD",
						ProductName: "Name",
						CreatedAt:   now,
						UpdatedAt:   now,
					}, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				updateCartItemQtyFunc: func(_ context.Context, _ sqlc.UpdateCartItemQuantityParams) (sqlc.CartItem, error) {
					return sqlc.CartItem{}, sql.ErrNoRows
				},
			},
			errIs: ErrCartItemNotFound,
		},
		{
			name: "db error",
			stub: stubQuerier{
				updateCartItemQtyFunc: func(_ context.Context, _ sqlc.UpdateCartItemQuantityParams) (sqlc.CartItem, error) {
					return sqlc.CartItem{}, sql.ErrConnDone
				},
			},
			errPrefix: "update cart item quantity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &CartItemRepository{queries: tt.stub}
			updated, err := repo.UpdateQuantity(context.Background(), cartID, sku, qty)

			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, updated)
				return
			}

			require.NoError(t, err)
			require.Equal(t, qty, updated.Quantity)
		})
	}
}

func TestCartItemRepositoryDelete(t *testing.T) {
	cartID := uuid.New()
	sku := "SKU-1"

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				deleteCartItemFunc: func(_ context.Context, arg sqlc.DeleteCartItemParams) (int64, error) {
					require.Equal(t, cartID, arg.CartID)
					require.Equal(t, sku, arg.Sku)
					return 1, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				deleteCartItemFunc: func(_ context.Context, _ sqlc.DeleteCartItemParams) (int64, error) {
					return 0, nil
				},
			},
			errIs: ErrCartItemNotFound,
		},
		{
			name: "query error",
			stub: stubQuerier{
				deleteCartItemFunc: func(_ context.Context, _ sqlc.DeleteCartItemParams) (int64, error) {
					return 0, sql.ErrConnDone
				},
			},
			errPrefix: "delete cart item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &CartItemRepository{queries: tt.stub}
			err := repo.Delete(context.Background(), cartID, sku)

			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

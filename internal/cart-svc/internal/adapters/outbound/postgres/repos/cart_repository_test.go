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

func TestCartRepositoryGetActiveByUserID(t *testing.T) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	cartID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				getActiveCartByUserIDFunc: func(_ context.Context, gotUserID uuid.UUID) (sqlc.Cart, error) {
					require.Equal(t, userID, gotUserID)
					return sqlc.Cart{
						CartID:    cartID,
						UserID:    userID,
						Status:    "active",
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
				getActiveCartByUserIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Cart, error) {
					return sqlc.Cart{}, sql.ErrNoRows
				},
			},
			errIs: ErrCartNotFound,
		},
		{
			name: "db error",
			stub: stubQuerier{
				getActiveCartByUserIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Cart, error) {
					return sqlc.Cart{}, sql.ErrConnDone
				},
			},
			errPrefix: "get active cart by user id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &CartRepository{queries: tt.stub}

			cart, err := repo.GetActiveByUserID(context.Background(), userID)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, cart)
				return
			}

			require.NoError(t, err)
			require.Equal(t, cartID, cart.CartID)
		})
	}
}

func TestCartRepositoryCreateActive(t *testing.T) {
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	cartID := uuid.New()
	currency := "USD"

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				createActiveCartFunc: func(_ context.Context, arg sqlc.CreateActiveCartParams) (sqlc.Cart, error) {
					require.Equal(t, userID, arg.UserID)
					require.Equal(t, currency, arg.Currency)
					return sqlc.Cart{
						CartID:    cartID,
						UserID:    userID,
						Status:    "active",
						Currency:  currency,
						CreatedAt: now,
						UpdatedAt: now,
					}, nil
				},
			},
		},
		{
			name: "already exists",
			stub: stubQuerier{
				createActiveCartFunc: func(_ context.Context, _ sqlc.CreateActiveCartParams) (sqlc.Cart, error) {
					return sqlc.Cart{}, &pgconn.PgError{Code: "23505"}
				},
			},
			errIs:     ErrCartAlreadyExists,
			errPrefix: "create active cart",
		},
		{
			name: "db error",
			stub: stubQuerier{
				createActiveCartFunc: func(_ context.Context, _ sqlc.CreateActiveCartParams) (sqlc.Cart, error) {
					return sqlc.Cart{}, sql.ErrConnDone
				},
			},
			errPrefix: "create active cart",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &CartRepository{queries: tt.stub}

			cart, err := repo.CreateActive(context.Background(), userID, currency)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, cart)
				return
			}

			require.NoError(t, err)
			require.Equal(t, cartID, cart.CartID)
		})
	}
}

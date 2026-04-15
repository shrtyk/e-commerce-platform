package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/sqlc"
)

type CartRepository struct {
	queries sqlc.Querier
}

func NewCartRepository(db *sql.DB) *CartRepository {
	return NewCartRepositoryFromQuerier(sqlc.New(db))
}

func NewCartRepositoryFromQuerier(queries sqlc.Querier) *CartRepository {
	return &CartRepository{queries: queries}
}

func NewCartRepositoryFromTx(tx *sql.Tx) *CartRepository {
	return NewCartRepositoryFromQuerier(sqlc.New(tx))
}

func (r *CartRepository) GetActiveByUserID(ctx context.Context, userID uuid.UUID) (sqlc.Cart, error) {
	cart, err := r.queries.GetActiveCartByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.Cart{}, ErrCartNotFound
		}

		return sqlc.Cart{}, fmt.Errorf("get active cart by user id %q: %w", userID.String(), err)
	}

	return cart, nil
}

func (r *CartRepository) CreateActive(ctx context.Context, userID uuid.UUID, currency string) (sqlc.Cart, error) {
	cart, err := r.queries.CreateActiveCart(ctx, sqlc.CreateActiveCartParams{
		UserID:   userID,
		Currency: currency,
	})
	if err != nil {
		mappedErr := mapCartWriteErr(err)
		return sqlc.Cart{}, fmt.Errorf("create active cart: %w", mappedErr)
	}

	return cart, nil
}

func mapCartWriteErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok && pgErr.Code == "23505" {
		return ErrCartAlreadyExists
	}

	return err
}

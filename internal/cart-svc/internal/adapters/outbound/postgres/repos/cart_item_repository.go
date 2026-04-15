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

type CartItemRepository struct {
	queries sqlc.Querier
}

func NewCartItemRepository(db *sql.DB) *CartItemRepository {
	return NewCartItemRepositoryFromQuerier(sqlc.New(db))
}

func NewCartItemRepositoryFromQuerier(queries sqlc.Querier) *CartItemRepository {
	return &CartItemRepository{queries: queries}
}

func NewCartItemRepositoryFromTx(tx *sql.Tx) *CartItemRepository {
	return NewCartItemRepositoryFromQuerier(sqlc.New(tx))
}

func (r *CartItemRepository) ListByCartID(ctx context.Context, cartID uuid.UUID) ([]sqlc.CartItem, error) {
	items, err := r.queries.ListCartItemsByCartID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("list cart items for cart id %q: %w", cartID.String(), err)
	}

	return items, nil
}

func (r *CartItemRepository) Insert(ctx context.Context, params sqlc.InsertCartItemParams) (sqlc.CartItem, error) {
	item, err := r.queries.InsertCartItem(ctx, params)
	if err != nil {
		mappedErr := mapCartItemWriteErr(err)
		return sqlc.CartItem{}, fmt.Errorf("insert cart item: %w", mappedErr)
	}

	return item, nil
}

func (r *CartItemRepository) UpdateQuantity(ctx context.Context, cartID uuid.UUID, sku string, quantity int32) (sqlc.CartItem, error) {
	item, err := r.queries.UpdateCartItemQuantity(ctx, sqlc.UpdateCartItemQuantityParams{
		Quantity: quantity,
		CartID:   cartID,
		Sku:      sku,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.CartItem{}, ErrCartItemNotFound
		}

		return sqlc.CartItem{}, fmt.Errorf("update cart item quantity: %w", err)
	}

	return item, nil
}

func (r *CartItemRepository) Delete(ctx context.Context, cartID uuid.UUID, sku string) error {
	rowsAffected, err := r.queries.DeleteCartItem(ctx, sqlc.DeleteCartItemParams{
		CartID: cartID,
		Sku:    sku,
	})
	if err != nil {
		return fmt.Errorf("delete cart item: %w", err)
	}

	if rowsAffected == 0 {
		return ErrCartItemNotFound
	}

	return nil
}

func mapCartItemWriteErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23505":
			return ErrCartItemAlreadyExists
		case "23503":
			switch pgErr.ConstraintName {
			case "cart_items_cart_id_fkey":
				return ErrCartNotFound
			case "cart_items_sku_fkey":
				return ErrProductSnapshotNotFound
			}
		}
	}

	return err
}

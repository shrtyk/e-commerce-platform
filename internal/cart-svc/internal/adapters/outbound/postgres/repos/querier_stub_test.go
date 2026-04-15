package repos

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/sqlc"
)

type stubQuerier struct {
	createActiveCartFunc      func(ctx context.Context, arg sqlc.CreateActiveCartParams) (sqlc.Cart, error)
	getActiveCartByUserIDFunc func(ctx context.Context, userID uuid.UUID) (sqlc.Cart, error)
	listCartItemsByCartIDFunc func(ctx context.Context, cartID uuid.UUID) ([]sqlc.CartItem, error)
	insertCartItemFunc        func(ctx context.Context, arg sqlc.InsertCartItemParams) (sqlc.CartItem, error)
	updateCartItemQtyFunc     func(ctx context.Context, arg sqlc.UpdateCartItemQuantityParams) (sqlc.CartItem, error)
	deleteCartItemFunc        func(ctx context.Context, arg sqlc.DeleteCartItemParams) (int64, error)
	getProductBySKUFunc       func(ctx context.Context, sku string) (sqlc.ProductSnapshot, error)
	upsertProductSnapshotFunc func(ctx context.Context, arg sqlc.UpsertProductSnapshotParams) (sqlc.ProductSnapshot, error)
}

func (s stubQuerier) CreateActiveCart(ctx context.Context, arg sqlc.CreateActiveCartParams) (sqlc.Cart, error) {
	if s.createActiveCartFunc == nil {
		return sqlc.Cart{}, fmt.Errorf("unexpected CreateActiveCart call")
	}

	return s.createActiveCartFunc(ctx, arg)
}

func (s stubQuerier) GetActiveCartByUserID(ctx context.Context, userID uuid.UUID) (sqlc.Cart, error) {
	if s.getActiveCartByUserIDFunc == nil {
		return sqlc.Cart{}, fmt.Errorf("unexpected GetActiveCartByUserID call")
	}

	return s.getActiveCartByUserIDFunc(ctx, userID)
}

func (s stubQuerier) ListCartItemsByCartID(ctx context.Context, cartID uuid.UUID) ([]sqlc.CartItem, error) {
	if s.listCartItemsByCartIDFunc == nil {
		return nil, fmt.Errorf("unexpected ListCartItemsByCartID call")
	}

	return s.listCartItemsByCartIDFunc(ctx, cartID)
}

func (s stubQuerier) InsertCartItem(ctx context.Context, arg sqlc.InsertCartItemParams) (sqlc.CartItem, error) {
	if s.insertCartItemFunc == nil {
		return sqlc.CartItem{}, fmt.Errorf("unexpected InsertCartItem call")
	}

	return s.insertCartItemFunc(ctx, arg)
}

func (s stubQuerier) UpdateCartItemQuantity(ctx context.Context, arg sqlc.UpdateCartItemQuantityParams) (sqlc.CartItem, error) {
	if s.updateCartItemQtyFunc == nil {
		return sqlc.CartItem{}, fmt.Errorf("unexpected UpdateCartItemQuantity call")
	}

	return s.updateCartItemQtyFunc(ctx, arg)
}

func (s stubQuerier) DeleteCartItem(ctx context.Context, arg sqlc.DeleteCartItemParams) (int64, error) {
	if s.deleteCartItemFunc == nil {
		return 0, fmt.Errorf("unexpected DeleteCartItem call")
	}

	return s.deleteCartItemFunc(ctx, arg)
}

func (s stubQuerier) GetProductSnapshotBySKU(ctx context.Context, sku string) (sqlc.ProductSnapshot, error) {
	if s.getProductBySKUFunc == nil {
		return sqlc.ProductSnapshot{}, fmt.Errorf("unexpected GetProductSnapshotBySKU call")
	}

	return s.getProductBySKUFunc(ctx, sku)
}

func (s stubQuerier) UpsertProductSnapshot(ctx context.Context, arg sqlc.UpsertProductSnapshotParams) (sqlc.ProductSnapshot, error) {
	if s.upsertProductSnapshotFunc == nil {
		return sqlc.ProductSnapshot{}, fmt.Errorf("unexpected UpsertProductSnapshot call")
	}

	return s.upsertProductSnapshotFunc(ctx, arg)
}

func (s stubQuerier) WithTx(_ *sql.Tx) *sqlc.Queries {
	panic("not implemented")
}

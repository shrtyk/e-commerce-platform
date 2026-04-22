package repos

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/sqlc"
)

type stubQuerier struct {
	getProductByIDFunc    func(ctx context.Context, productID uuid.UUID) (sqlc.GetProductByIDRow, error)
	getProductBySKUFunc   func(ctx context.Context, sku string) (sqlc.GetProductBySKURow, error)
	getCurrencyByCodeFunc func(ctx context.Context, code string) (uuid.UUID, error)
	listProductsFunc      func(ctx context.Context, arg sqlc.ListProductsParams) ([]sqlc.ListProductsRow, error)
	createProductFunc     func(ctx context.Context, arg sqlc.CreateProductParams) (sqlc.CreateProductRow, error)
	createStockFunc       func(ctx context.Context, arg sqlc.CreateStockRecordParams) (sqlc.StockRecord, error)
	createReservationFunc func(ctx context.Context, arg sqlc.CreateStockReservationParams) (sqlc.StockReservation, error)
	getStockFunc          func(ctx context.Context, productID uuid.UUID) (sqlc.StockRecord, error)
	getStockForUpdateFunc func(ctx context.Context, productID uuid.UUID) (sqlc.StockRecord, error)
	listReservationsFunc  func(ctx context.Context, orderID uuid.UUID) ([]sqlc.StockReservation, error)
	deleteReservationsFunc func(ctx context.Context, orderID uuid.UUID) error
	updateStockFunc       func(ctx context.Context, arg sqlc.UpdateStockRecordParams) (sqlc.StockRecord, error)
	updateProductFunc     func(ctx context.Context, arg sqlc.UpdateProductParams) (sqlc.UpdateProductRow, error)
	deleteProductFunc     func(ctx context.Context, productID uuid.UUID) (sqlc.Product, error)
	appendOutboxFunc      func(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error)
	claimOutboxFunc       func(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	claimStaleOutboxFunc  func(ctx context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	markRetryableFunc     func(ctx context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error)
	markDeadFunc          func(ctx context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error)
	markPublishedFunc     func(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error)
}

func (s stubQuerier) GetProductByID(ctx context.Context, productID uuid.UUID) (sqlc.GetProductByIDRow, error) {
	if s.getProductByIDFunc == nil {
		return sqlc.GetProductByIDRow{}, fmt.Errorf("unexpected GetProductByID call")
	}

	return s.getProductByIDFunc(ctx, productID)
}

func (s stubQuerier) GetProductBySKU(ctx context.Context, sku string) (sqlc.GetProductBySKURow, error) {
	if s.getProductBySKUFunc == nil {
		return sqlc.GetProductBySKURow{}, fmt.Errorf("unexpected GetProductBySKU call")
	}

	return s.getProductBySKUFunc(ctx, sku)
}

func (s stubQuerier) GetCurrencyByCode(ctx context.Context, code string) (uuid.UUID, error) {
	if s.getCurrencyByCodeFunc == nil {
		return uuid.Nil, fmt.Errorf("unexpected GetCurrencyByCode call")
	}

	return s.getCurrencyByCodeFunc(ctx, code)
}

func (s stubQuerier) ListProducts(ctx context.Context, arg sqlc.ListProductsParams) ([]sqlc.ListProductsRow, error) {
	if s.listProductsFunc == nil {
		return nil, fmt.Errorf("unexpected ListProducts call")
	}

	return s.listProductsFunc(ctx, arg)
}

func (s stubQuerier) CreateProduct(ctx context.Context, arg sqlc.CreateProductParams) (sqlc.CreateProductRow, error) {
	if s.createProductFunc == nil {
		return sqlc.CreateProductRow{}, fmt.Errorf("unexpected CreateProduct call")
	}

	return s.createProductFunc(ctx, arg)
}

func (s stubQuerier) CreateStockRecord(ctx context.Context, arg sqlc.CreateStockRecordParams) (sqlc.StockRecord, error) {
	if s.createStockFunc == nil {
		return sqlc.StockRecord{}, fmt.Errorf("unexpected CreateStockRecord call")
	}

	return s.createStockFunc(ctx, arg)
}

func (s stubQuerier) CreateStockReservation(ctx context.Context, arg sqlc.CreateStockReservationParams) (sqlc.StockReservation, error) {
	if s.createReservationFunc == nil {
		return sqlc.StockReservation{}, fmt.Errorf("unexpected CreateStockReservation call")
	}

	return s.createReservationFunc(ctx, arg)
}

func (s stubQuerier) GetStockRecordByProductID(ctx context.Context, productID uuid.UUID) (sqlc.StockRecord, error) {
	if s.getStockFunc == nil {
		return sqlc.StockRecord{}, fmt.Errorf("unexpected GetStockRecordByProductID call")
	}

	return s.getStockFunc(ctx, productID)
}

func (s stubQuerier) GetStockRecordByProductIDForUpdate(ctx context.Context, productID uuid.UUID) (sqlc.StockRecord, error) {
	if s.getStockForUpdateFunc == nil {
		return sqlc.StockRecord{}, fmt.Errorf("unexpected GetStockRecordByProductIDForUpdate call")
	}

	return s.getStockForUpdateFunc(ctx, productID)
}

func (s stubQuerier) ListStockReservationsByOrderID(ctx context.Context, orderID uuid.UUID) ([]sqlc.StockReservation, error) {
	if s.listReservationsFunc == nil {
		return nil, fmt.Errorf("unexpected ListStockReservationsByOrderID call")
	}

	return s.listReservationsFunc(ctx, orderID)
}

func (s stubQuerier) DeleteStockReservationsByOrderID(ctx context.Context, orderID uuid.UUID) error {
	if s.deleteReservationsFunc == nil {
		return fmt.Errorf("unexpected DeleteStockReservationsByOrderID call")
	}

	return s.deleteReservationsFunc(ctx, orderID)
}

func (s stubQuerier) UpdateStockRecord(ctx context.Context, arg sqlc.UpdateStockRecordParams) (sqlc.StockRecord, error) {
	if s.updateStockFunc == nil {
		return sqlc.StockRecord{}, fmt.Errorf("unexpected UpdateStockRecord call")
	}

	return s.updateStockFunc(ctx, arg)
}

func (s stubQuerier) UpdateProduct(ctx context.Context, arg sqlc.UpdateProductParams) (sqlc.UpdateProductRow, error) {
	if s.updateProductFunc == nil {
		return sqlc.UpdateProductRow{}, fmt.Errorf("unexpected UpdateProduct call")
	}

	return s.updateProductFunc(ctx, arg)
}

func (s stubQuerier) DeleteProduct(ctx context.Context, productID uuid.UUID) (sqlc.Product, error) {
	if s.deleteProductFunc == nil {
		return sqlc.Product{}, fmt.Errorf("unexpected DeleteProduct call")
	}

	return s.deleteProductFunc(ctx, productID)
}

func (s stubQuerier) AppendOutboxRecord(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error) {
	if s.appendOutboxFunc == nil {
		return sqlc.OutboxRecord{}, fmt.Errorf("unexpected AppendOutboxRecord call")
	}

	return s.appendOutboxFunc(ctx, arg)
}

func (s stubQuerier) ClaimPendingOutboxRecords(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
	if s.claimOutboxFunc == nil {
		return nil, fmt.Errorf("unexpected ClaimPendingOutboxRecords call")
	}

	return s.claimOutboxFunc(ctx, arg)
}

func (s stubQuerier) ClaimStaleInProgressOutboxRecords(ctx context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
	if s.claimStaleOutboxFunc == nil {
		return nil, fmt.Errorf("unexpected ClaimStaleInProgressOutboxRecords call")
	}

	return s.claimStaleOutboxFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordRetryableFailure(ctx context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error) {
	if s.markRetryableFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordRetryableFailure call")
	}

	return s.markRetryableFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordDead(ctx context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error) {
	if s.markDeadFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordDead call")
	}

	return s.markDeadFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordPublished(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
	if s.markPublishedFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordPublished call")
	}

	return s.markPublishedFunc(ctx, arg)
}

func (s stubQuerier) WithTx(_ *sql.Tx) *sqlc.Queries {
	panic("not implemented")
}

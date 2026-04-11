package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
)

type StockRepository struct {
	queries sqlc.Querier
}

func NewStockRepository(db *sql.DB) *StockRepository {
	return NewStockRepositoryFromQuerier(sqlc.New(db))
}

func NewStockRepositoryFromQuerier(queries sqlc.Querier) *StockRepository {
	return &StockRepository{queries: queries}
}

func NewStockRepositoryFromTx(tx *sql.Tx) *StockRepository {
	return NewStockRepositoryFromQuerier(sqlc.New(tx))
}

func (r *StockRepository) Create(ctx context.Context, stock domain.StockRecord) (domain.StockRecord, error) {
	created, err := r.queries.CreateStockRecord(ctx, sqlc.CreateStockRecordParams{
		ProductID: stock.ProductID,
		Quantity:  stock.Quantity,
		Reserved:  stock.Reserved,
	})
	if err != nil {
		return domain.StockRecord{}, fmt.Errorf("create stock record: %w", mapStockWriteErr(err))
	}

	return mapStockRecord(created), nil
}

func (r *StockRepository) GetByProductID(ctx context.Context, productID uuid.UUID) (domain.StockRecord, error) {
	row, err := r.queries.GetStockRecordByProductID(ctx, productID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.StockRecord{}, outbound.ErrStockRecordNotFound
		}

		return domain.StockRecord{}, fmt.Errorf("get stock record by product id %q: %w", productID.String(), err)
	}

	return mapStockRecord(row), nil
}

func (r *StockRepository) Update(ctx context.Context, stock domain.StockRecord) (domain.StockRecord, error) {
	updated, err := r.queries.UpdateStockRecord(ctx, sqlc.UpdateStockRecordParams{
		Quantity:      stock.Quantity,
		Reserved:      stock.Reserved,
		StockRecordID: stock.StockRecordID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.StockRecord{}, outbound.ErrStockRecordNotFound
		}

		return domain.StockRecord{}, fmt.Errorf("update stock record: %w", mapStockWriteErr(err))
	}

	return mapStockRecord(updated), nil
}

func mapStockWriteErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23503":
			return outbound.ErrProductNotFound
		case "23514":
			return outbound.ErrInvalidStockUpdate
		}
	}

	return err
}

func mapStockRecord(record sqlc.StockRecord) domain.StockRecord {
	available := record.Quantity - record.Reserved
	if record.Available.Valid {
		available = record.Available.Int32
	}

	status := domain.StockRecordStatusOutOfStock
	if available > 0 {
		status = domain.StockRecordStatusInStock
	}

	return domain.StockRecord{
		StockRecordID: record.StockRecordID,
		ProductID:     record.ProductID,
		Quantity:      record.Quantity,
		Reserved:      record.Reserved,
		Available:     available,
		Status:        status,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}

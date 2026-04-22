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

type ProductRepository struct {
	queries sqlc.Querier
}

func NewProductRepository(db *sql.DB) *ProductRepository {
	return NewProductRepositoryFromQuerier(sqlc.New(db))
}

func NewProductRepositoryFromQuerier(queries sqlc.Querier) *ProductRepository {
	return &ProductRepository{queries: queries}
}

func NewProductRepositoryFromTx(tx *sql.Tx) *ProductRepository {
	return NewProductRepositoryFromQuerier(sqlc.New(tx))
}

func (r *ProductRepository) GetByID(ctx context.Context, productID uuid.UUID) (domain.Product, error) {
	row, err := r.queries.GetProductByID(ctx, productID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Product{}, outbound.ErrProductNotFound
		}

		return domain.Product{}, fmt.Errorf("get product by id %q: %w", productID.String(), err)
	}

	return mapProduct(row.Product, &row.Currency), nil
}

func (r *ProductRepository) GetBySKU(ctx context.Context, sku string) (domain.Product, error) {
	row, err := r.queries.GetProductBySKU(ctx, sku)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Product{}, outbound.ErrProductNotFound
		}

		return domain.Product{}, fmt.Errorf("get product by sku %q: %w", sku, err)
	}

	return mapProduct(row.Product, &row.Currency), nil
}

func (r *ProductRepository) GetCurrencyByCode(ctx context.Context, code string) (uuid.UUID, error) {
	currencyID, err := r.queries.GetCurrencyByCode(ctx, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, outbound.ErrInvalidCurrency
		}

		return uuid.Nil, fmt.Errorf("get currency by code %q: %w", code, err)
	}

	return currencyID, nil
}

func (r *ProductRepository) List(ctx context.Context, params outbound.ProductListParams) ([]domain.Product, error) {
	rows, err := r.queries.ListProducts(ctx, sqlc.ListProductsParams{
		Limit:  params.Limit,
		Offset: params.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}

	products := make([]domain.Product, 0, len(rows))
	for _, row := range rows {
		products = append(products, mapProduct(row.Product, &row.Currency))
	}

	return products, nil
}

func (r *ProductRepository) Create(ctx context.Context, product domain.Product) (domain.Product, error) {
	created, err := r.queries.CreateProduct(ctx, sqlc.CreateProductParams{
		Sku:         product.SKU,
		Name:        product.Name,
		Description: toNullString(product.Description),
		Price:       product.Price,
		CurrencyID:  product.CurrencyID,
		CategoryID:  toNullUUID(product.CategoryID),
		Status:      string(product.Status),
	})
	if err != nil {
		return domain.Product{}, fmt.Errorf("create product: %w", mapWriteErr(err))
	}

	return mapProduct(created.Product, &created.Currency), nil
}

func (r *ProductRepository) Update(ctx context.Context, product domain.Product) (domain.Product, error) {
	updated, err := r.queries.UpdateProduct(ctx, sqlc.UpdateProductParams{
		ProductID:   product.ID,
		Sku:         product.SKU,
		Name:        product.Name,
		Description: toNullString(product.Description),
		Price:       product.Price,
		CurrencyID:  product.CurrencyID,
		CategoryID:  toNullUUID(product.CategoryID),
		Status:      string(product.Status),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Product{}, outbound.ErrProductNotFound
		}

		return domain.Product{}, fmt.Errorf("update product: %w", mapWriteErr(err))
	}

	return mapProduct(updated.Product, &updated.Currency), nil
}

func (r *ProductRepository) Delete(ctx context.Context, productID uuid.UUID) (domain.Product, error) {
	deleted, err := r.queries.DeleteProduct(ctx, productID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Product{}, outbound.ErrProductNotFound
		}

		return domain.Product{}, fmt.Errorf("delete product: %w", err)
	}

	// Delete returns product data without joined currency metadata.
	return mapProduct(deleted, nil), nil
}

func mapWriteErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23505":
			return outbound.ErrProductAlreadyExists
		case "23503":
			return outbound.ErrInvalidCurrency
		}
	}

	return err
}

func mapProduct(product sqlc.Product, currency *sqlc.Currency) domain.Product {
	mapped := domain.Product{
		ID:         product.ProductID,
		SKU:        product.Sku,
		Name:       product.Name,
		Price:      product.Price,
		CurrencyID: product.CurrencyID,
		Status:     domain.ProductStatus(product.Status),
		CreatedAt:  product.CreatedAt,
		UpdatedAt:  product.UpdatedAt,
	}

	if product.Description.Valid {
		mapped.Description = product.Description.String
	}

	if product.CategoryID.Valid {
		categoryID := product.CategoryID.UUID
		mapped.CategoryID = &categoryID
	}

	if currency != nil {
		mapped.Currency = currency.Code
		mapped.CurrencyName = currency.Name
		mapped.CurrencyDecimals = currency.Decimals
	}

	return mapped
}

func toNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func toNullUUID(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}

	return uuid.NullUUID{UUID: *value, Valid: true}
}

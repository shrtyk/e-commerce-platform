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

func TestProductRepositoryGetByID(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	productID := uuid.New()
	currencyID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
		checkFunc func(t *testing.T, product domain.Product)
	}{
		{
			name: "found",
			stub: stubQuerier{
				getProductByIDFunc: func(_ context.Context, gotProductID uuid.UUID) (sqlc.GetProductByIDRow, error) {
					require.Equal(t, productID, gotProductID)
					return sqlc.GetProductByIDRow{Product: sqlc.Product{
						ProductID:  productID,
						Sku:        "SKU-1",
						Name:       "Name",
						Price:      1500,
						CurrencyID: currencyID,
						Status:     string(domain.ProductStatusPublished),
						CreatedAt:  now,
						UpdatedAt:  now,
					}, Currency: sqlc.Currency{ID: currencyID, Code: "USD", Name: "US Dollar", Decimals: 2}}, nil
				},
			},
			checkFunc: func(t *testing.T, product domain.Product) {
				require.Equal(t, productID, product.ID)
				require.Equal(t, "SKU-1", product.SKU)
				require.Equal(t, currencyID, product.CurrencyID)
				require.Equal(t, "USD", product.Currency)
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				getProductByIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.GetProductByIDRow, error) {
					return sqlc.GetProductByIDRow{}, sql.ErrNoRows
				},
			},
			errIs: outbound.ErrProductNotFound,
		},
		{
			name: "db error",
			stub: stubQuerier{
				getProductByIDFunc: func(_ context.Context, _ uuid.UUID) (sqlc.GetProductByIDRow, error) {
					return sqlc.GetProductByIDRow{}, sql.ErrConnDone
				},
			},
			errPrefix: "get product by id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductRepository{queries: tt.stub}

			product, err := repo.GetByID(context.Background(), productID)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, product)
				return
			}

			require.NoError(t, err)
			tt.checkFunc(t, product)
		})
	}
}

func TestProductRepositoryGetBySKU(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	productID := uuid.New()
	currencyID := uuid.New()
	sku := "SKU-42"

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "found",
			stub: stubQuerier{
				getProductBySKUFunc: func(_ context.Context, gotSKU string) (sqlc.GetProductBySKURow, error) {
					require.Equal(t, sku, gotSKU)
					return sqlc.GetProductBySKURow{Product: sqlc.Product{
						ProductID:  productID,
						Sku:        sku,
						Name:       "Product 42",
						Price:      999,
						CurrencyID: currencyID,
						Status:     string(domain.ProductStatusDraft),
						CreatedAt:  now,
						UpdatedAt:  now,
					}, Currency: sqlc.Currency{ID: currencyID, Code: "USD", Name: "US Dollar", Decimals: 2}}, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				getProductBySKUFunc: func(_ context.Context, _ string) (sqlc.GetProductBySKURow, error) {
					return sqlc.GetProductBySKURow{}, sql.ErrNoRows
				},
			},
			errIs: outbound.ErrProductNotFound,
		},
		{
			name: "db error",
			stub: stubQuerier{
				getProductBySKUFunc: func(_ context.Context, _ string) (sqlc.GetProductBySKURow, error) {
					return sqlc.GetProductBySKURow{}, sql.ErrConnDone
				},
			},
			errPrefix: "get product by sku",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductRepository{queries: tt.stub}
			product, err := repo.GetBySKU(context.Background(), sku)

			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, product)
				return
			}

			require.NoError(t, err)
			require.Equal(t, productID, product.ID)
		})
	}
}

func TestProductRepositoryGetCurrencyByCode(t *testing.T) {
	currencyID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "found",
			stub: stubQuerier{
				getCurrencyByCodeFunc: func(_ context.Context, gotCode string) (uuid.UUID, error) {
					require.Equal(t, "USD", gotCode)
					return currencyID, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				getCurrencyByCodeFunc: func(_ context.Context, _ string) (uuid.UUID, error) {
					return uuid.Nil, sql.ErrNoRows
				},
			},
			errIs: outbound.ErrInvalidCurrency,
		},
		{
			name: "db error",
			stub: stubQuerier{
				getCurrencyByCodeFunc: func(_ context.Context, _ string) (uuid.UUID, error) {
					return uuid.Nil, sql.ErrConnDone
				},
			},
			errPrefix: "get currency by code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductRepository{queries: tt.stub}
			id, err := repo.GetCurrencyByCode(context.Background(), "USD")

			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Equal(t, uuid.Nil, id)
				return
			}

			require.NoError(t, err)
			require.Equal(t, currencyID, id)
		})
	}
}

func TestProductRepositoryList(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	currencyID := uuid.New()
	params := outbound.ProductListParams{Limit: 2, Offset: 4}

	tests := []struct {
		name  string
		stub  stubQuerier
		err   string
		count int
	}{
		{
			name: "pagination",
			stub: stubQuerier{
				listProductsFunc: func(_ context.Context, got sqlc.ListProductsParams) ([]sqlc.ListProductsRow, error) {
					require.Equal(t, params.Limit, got.Limit)
					require.Equal(t, params.Offset, got.Offset)
					return []sqlc.ListProductsRow{
						{Product: sqlc.Product{ProductID: uuid.New(), Sku: "SKU-A", Name: "A", Price: 100, CurrencyID: currencyID, Status: "draft", CreatedAt: now, UpdatedAt: now}, Currency: sqlc.Currency{ID: currencyID, Code: "USD", Name: "US Dollar", Decimals: 2}},
						{Product: sqlc.Product{ProductID: uuid.New(), Sku: "SKU-B", Name: "B", Price: 200, CurrencyID: currencyID, Status: "published", CreatedAt: now, UpdatedAt: now}, Currency: sqlc.Currency{ID: currencyID, Code: "USD", Name: "US Dollar", Decimals: 2}},
					}, nil
				},
			},
			count: 2,
		},
		{
			name: "query error",
			stub: stubQuerier{
				listProductsFunc: func(_ context.Context, _ sqlc.ListProductsParams) ([]sqlc.ListProductsRow, error) {
					return nil, sql.ErrConnDone
				},
			},
			err: "list products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductRepository{queries: tt.stub}
			products, err := repo.List(context.Background(), params)

			if tt.err != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.err)
				require.Nil(t, products)
				return
			}

			require.NoError(t, err)
			require.Len(t, products, tt.count)
			require.Equal(t, "SKU-A", products[0].SKU)
			require.Equal(t, domain.ProductStatusPublished, products[1].Status)
		})
	}
}

func TestProductRepositoryCreate(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	categoryID := uuid.New()
	currencyID := uuid.New()

	tests := []struct {
		name      string
		input     domain.Product
		stub      stubQuerier
		errIs     error
		errPrefix string
		checkFunc func(t *testing.T, product domain.Product)
	}{
		{
			name: "success",
			input: domain.Product{
				SKU:         "SKU-NEW",
				Name:        "New Product",
				Description: "desc",
				Price:       1500,
				CurrencyID:  currencyID,
				CategoryID:  &categoryID,
				Status:      domain.ProductStatusDraft,
			},
			stub: stubQuerier{
				createProductFunc: func(_ context.Context, arg sqlc.CreateProductParams) (sqlc.CreateProductRow, error) {
					require.Equal(t, "SKU-NEW", arg.Sku)
					require.Equal(t, "desc", arg.Description.String)
					require.True(t, arg.Description.Valid)
					require.True(t, arg.CategoryID.Valid)
					require.Equal(t, currencyID, arg.CurrencyID)
					createdID := uuid.New()
					return sqlc.CreateProductRow{
						Product: sqlc.Product{
							ProductID:   createdID,
							Sku:         arg.Sku,
							Name:        arg.Name,
							Description: arg.Description,
							Price:       arg.Price,
							CurrencyID:  arg.CurrencyID,
							CategoryID:  arg.CategoryID,
							Status:      arg.Status,
							CreatedAt:   now,
							UpdatedAt:   now,
						},
						Currency: sqlc.Currency{ID: arg.CurrencyID, Code: "USD", Name: "US Dollar", Decimals: 2},
					}, nil
				},
			},
			checkFunc: func(t *testing.T, product domain.Product) {
				require.NotEqual(t, uuid.Nil, product.ID)
				require.Equal(t, "SKU-NEW", product.SKU)
				require.NotNil(t, product.CategoryID)
				require.Equal(t, "USD", product.Currency)
				require.Equal(t, int32(2), product.CurrencyDecimals)
			},
		},
		{
			name: "duplicate",
			input: domain.Product{
				SKU:        "SKU-EXISTS",
				Name:       "Product",
				Price:      100,
				CurrencyID: currencyID,
				Status:     domain.ProductStatusDraft,
			},
			stub: stubQuerier{
				createProductFunc: func(_ context.Context, _ sqlc.CreateProductParams) (sqlc.CreateProductRow, error) {
					return sqlc.CreateProductRow{}, &pgconn.PgError{Code: "23505"}
				},
			},
			errIs:     outbound.ErrProductAlreadyExists,
			errPrefix: "create product",
		},
		{
			name: "invalid currency",
			input: domain.Product{
				SKU:        "SKU-BAD-CURRENCY",
				Name:       "Product",
				Price:      100,
				CurrencyID: currencyID,
				Status:     domain.ProductStatusDraft,
			},
			stub: stubQuerier{
				createProductFunc: func(_ context.Context, _ sqlc.CreateProductParams) (sqlc.CreateProductRow, error) {
					return sqlc.CreateProductRow{}, &pgconn.PgError{Code: "23503"}
				},
			},
			errIs:     outbound.ErrInvalidCurrency,
			errPrefix: "create product",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductRepository{queries: tt.stub}
			product, err := repo.Create(context.Background(), tt.input)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, product)
				return
			}

			require.NoError(t, err)
			tt.checkFunc(t, product)
		})
	}
}

func TestProductRepositoryUpdate(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	productID := uuid.New()
	currencyID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				updateProductFunc: func(_ context.Context, arg sqlc.UpdateProductParams) (sqlc.UpdateProductRow, error) {
					require.Equal(t, productID, arg.ProductID)
					require.Equal(t, currencyID, arg.CurrencyID)
					return sqlc.UpdateProductRow{
						Product: sqlc.Product{
							ProductID:  arg.ProductID,
							Sku:        arg.Sku,
							Name:       arg.Name,
							Price:      arg.Price,
							CurrencyID: arg.CurrencyID,
							Status:     arg.Status,
							CreatedAt:  now,
							UpdatedAt:  now,
						},
						Currency: sqlc.Currency{ID: arg.CurrencyID, Code: "USD", Name: "US Dollar", Decimals: 2},
					}, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				updateProductFunc: func(_ context.Context, _ sqlc.UpdateProductParams) (sqlc.UpdateProductRow, error) {
					return sqlc.UpdateProductRow{}, sql.ErrNoRows
				},
			},
			errIs: outbound.ErrProductNotFound,
		},
		{
			name: "duplicate",
			stub: stubQuerier{
				updateProductFunc: func(_ context.Context, _ sqlc.UpdateProductParams) (sqlc.UpdateProductRow, error) {
					return sqlc.UpdateProductRow{}, &pgconn.PgError{Code: "23505"}
				},
			},
			errIs:     outbound.ErrProductAlreadyExists,
			errPrefix: "update product",
		},
		{
			name: "invalid currency",
			stub: stubQuerier{
				updateProductFunc: func(_ context.Context, _ sqlc.UpdateProductParams) (sqlc.UpdateProductRow, error) {
					return sqlc.UpdateProductRow{}, &pgconn.PgError{Code: "23503"}
				},
			},
			errIs:     outbound.ErrInvalidCurrency,
			errPrefix: "update product",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductRepository{queries: tt.stub}
			product, err := repo.Update(context.Background(), domain.Product{
				ID:         productID,
				SKU:        "SKU-U",
				Name:       "Updated",
				Price:      777,
				CurrencyID: currencyID,
				Status:     domain.ProductStatusArchived,
			})
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, product)
				return
			}

			require.NoError(t, err)
			require.Equal(t, productID, product.ID)
			require.Equal(t, domain.ProductStatusArchived, product.Status)
			require.Equal(t, "USD", product.Currency)
		})
	}
}

func TestProductRepositoryDelete(t *testing.T) {
	now := time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC)
	productID := uuid.New()
	currencyID := uuid.New()

	tests := []struct {
		name      string
		stub      stubQuerier
		errIs     error
		errPrefix string
	}{
		{
			name: "success",
			stub: stubQuerier{
				deleteProductFunc: func(_ context.Context, gotProductID uuid.UUID) (sqlc.Product, error) {
					require.Equal(t, productID, gotProductID)
					return sqlc.Product{
						ProductID:  productID,
						Sku:        "SKU-D",
						Name:       "Deleted",
						Price:      333,
						CurrencyID: currencyID,
						Status:     "draft",
						CreatedAt:  now,
						UpdatedAt:  now,
					}, nil
				},
			},
		},
		{
			name: "not found",
			stub: stubQuerier{
				deleteProductFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Product, error) {
					return sqlc.Product{}, sql.ErrNoRows
				},
			},
			errIs: outbound.ErrProductNotFound,
		},
		{
			name: "db error",
			stub: stubQuerier{
				deleteProductFunc: func(_ context.Context, _ uuid.UUID) (sqlc.Product, error) {
					return sqlc.Product{}, sql.ErrConnDone
				},
			},
			errPrefix: "delete product",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ProductRepository{queries: tt.stub}
			product, err := repo.Delete(context.Background(), productID)
			if tt.errIs != nil || tt.errPrefix != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errPrefix != "" {
					require.ErrorContains(t, err, tt.errPrefix)
				}
				require.Zero(t, product)
				return
			}

			require.NoError(t, err)
			require.Equal(t, productID, product.ID)
		})
	}
}

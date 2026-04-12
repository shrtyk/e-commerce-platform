package catalog

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound/mocks"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateProduct(t *testing.T) {
	productID := uuid.New()
	currencyID := uuid.New()

	tests := []struct {
		name          string
		input         CreateProductInput
		setupMocks    func(*outboundmocks.MockProductRepository, *outboundmocks.MockStockRepository, *outboundmocks.MockEventPublisher)
		assertNoCalls func(t *testing.T, products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository, publisher *outboundmocks.MockEventPublisher)
		errIs         error
		errContains   string
		expectProduct bool
		expectStock   bool
	}{
		{
			name: "success",
			input: CreateProductInput{
				Product: domain.Product{
					SKU:        "SKU-1",
					Name:       "Product",
					Price:      1000,
					CurrencyID: currencyID,
					Currency:   "USD",
					Status:     domain.ProductStatusPublished,
				},
				InitialQuantity: 10,
			},
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository, publisher *outboundmocks.MockEventPublisher) {
				products.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					Return(domain.Product{ID: productID, SKU: "SKU-1", Name: "Product", Price: 1000, CurrencyID: currencyID, Currency: "USD", Status: domain.ProductStatusPublished}, nil)
				stocks.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					RunAndReturn(func(_ context.Context, stock domain.StockRecord) (domain.StockRecord, error) {
						require.Equal(t, productID, stock.ProductID)
						require.Equal(t, int32(10), stock.Quantity)
						require.Equal(t, int32(0), stock.Reserved)

						return domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 10, Reserved: 0, Available: 10, Status: domain.StockRecordStatusInStock}, nil
					})
				publisher.EXPECT().
					Publish(testifymock.Anything, testifymock.Anything).
					RunAndReturn(func(_ context.Context, event domain.DomainEvent) error {
						require.NotNil(t, event)
						require.Equal(t, "catalog.product.created", event.EventName)
						require.Equal(t, "catalog.product.events", event.Topic)
						require.Equal(t, productID.String(), event.AggregateID)
						return nil
					})
			},
			expectProduct: true,
			expectStock:   true,
		},
		{
			name: "invalid input",
			input: CreateProductInput{
				Product:         domain.Product{},
				InitialQuantity: -1,
			},
			errIs: ErrInvalidCreateProductInput,
		},
		{
			name: "duplicate product",
			input: CreateProductInput{
				Product: domain.Product{SKU: "SKU-1", Name: "Product", Price: 1000, CurrencyID: currencyID},
			},
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository, publisher *outboundmocks.MockEventPublisher) {
				products.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					Return(domain.Product{}, outbound.ErrProductAlreadyExists)
			},
			assertNoCalls: func(t *testing.T, _ *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository, publisher *outboundmocks.MockEventPublisher) {
				stocks.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
				publisher.AssertNotCalled(t, "Publish", testifymock.Anything, testifymock.Anything)
			},
			errIs: outbound.ErrProductAlreadyExists,
		},
		{
			name: "publish event error",
			input: CreateProductInput{
				Product: domain.Product{SKU: "SKU-1", Name: "Product", Price: 1000, CurrencyID: currencyID, Currency: "USD"},
			},
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository, publisher *outboundmocks.MockEventPublisher) {
				products.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					Return(domain.Product{ID: productID, SKU: "SKU-1", Name: "Product", Price: 1000, CurrencyID: currencyID, Currency: "USD"}, nil)
				stocks.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					Return(domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 0, Reserved: 0, Available: 0, Status: domain.StockRecordStatusOutOfStock}, nil)
				publisher.EXPECT().
					Publish(testifymock.Anything, testifymock.Anything).
					Return(errors.New("broker unavailable"))
			},
			errContains: "publish product created event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := outboundmocks.NewMockProductRepository(t)
			stocks := outboundmocks.NewMockStockRepository(t)
			publisher := outboundmocks.NewMockEventPublisher(t)
			provider := newStubProvider(products, stocks, publisher)
			svc := NewCatalogService(products, stocks, publisher, provider, "product-svc")

			if tt.setupMocks != nil {
				tt.setupMocks(products, stocks, publisher)
			}

			result, err := svc.CreateProduct(context.Background(), tt.input)
			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.assertNoCalls != nil {
					tt.assertNoCalls(t, products, stocks, publisher)
				}
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				require.Zero(t, result)
				return
			}

			require.NoError(t, err)
			if tt.expectProduct {
				require.NotEqual(t, uuid.Nil, result.Product.ID)
			}
			if tt.expectStock {
				require.NotEqual(t, uuid.Nil, result.Stock.StockRecordID)
			}
		})
	}
}

func TestGetProduct(t *testing.T) {
	productID := uuid.New()
	product := domain.Product{ID: productID, SKU: "SKU-1", Name: "Product"}
	stock := domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 10, Reserved: 2, Available: 8}

	tests := []struct {
		name          string
		productID     uuid.UUID
		setupMocks    func(*outboundmocks.MockProductRepository, *outboundmocks.MockStockRepository)
		assertNoCalls func(t *testing.T, products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository)
		errIs         error
		errContains   string
	}{
		{
			name:      "success",
			productID: productID,
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository) {
				products.EXPECT().GetByID(testifymock.Anything, productID).Return(product, nil)
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(stock, nil)
			},
		},
		{
			name:      "product not found",
			productID: productID,
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository) {
				products.EXPECT().GetByID(testifymock.Anything, productID).Return(domain.Product{}, outbound.ErrProductNotFound)
			},
			assertNoCalls: func(t *testing.T, _ *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository) {
				stocks.AssertNotCalled(t, "GetByProductID", testifymock.Anything, testifymock.Anything)
			},
			errIs: outbound.ErrProductNotFound,
		},
		{
			name:      "stock repo error",
			productID: productID,
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository) {
				products.EXPECT().GetByID(testifymock.Anything, productID).Return(product, nil)
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(domain.StockRecord{}, errors.New("db down"))
			},
			errContains: "get stock by product id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := outboundmocks.NewMockProductRepository(t)
			stocks := outboundmocks.NewMockStockRepository(t)
			publisher := outboundmocks.NewMockEventPublisher(t)
			svc := NewCatalogService(products, stocks, publisher, newStubProvider(products, stocks, publisher), "product-svc")

			if tt.setupMocks != nil {
				tt.setupMocks(products, stocks)
			}

			result, err := svc.GetProduct(context.Background(), tt.productID)
			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.assertNoCalls != nil {
					tt.assertNoCalls(t, products, stocks)
				}
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				require.Zero(t, result)
				return
			}

			require.NoError(t, err)
			require.Equal(t, productID, result.Product.ID)
			require.Equal(t, int32(8), result.Stock.Available)
		})
	}
}

func TestListProducts(t *testing.T) {
	products := []domain.Product{{ID: uuid.New(), SKU: "SKU-1"}, {ID: uuid.New(), SKU: "SKU-2"}}

	tests := []struct {
		name        string
		params      outbound.ProductListParams
		setupMocks  func(*outboundmocks.MockProductRepository)
		errContains string
		expectCount int
	}{
		{
			name:   "success",
			params: outbound.ProductListParams{Limit: 10, Offset: 0},
			setupMocks: func(repo *outboundmocks.MockProductRepository) {
				repo.EXPECT().List(testifymock.Anything, outbound.ProductListParams{Limit: 10, Offset: 0}).Return(products, nil)
			},
			expectCount: 2,
		},
		{
			name:   "repo error",
			params: outbound.ProductListParams{Limit: 10, Offset: 0},
			setupMocks: func(repo *outboundmocks.MockProductRepository) {
				repo.EXPECT().List(testifymock.Anything, outbound.ProductListParams{Limit: 10, Offset: 0}).Return(nil, errors.New("query failed"))
			},
			errContains: "list products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockProductRepository(t)
			stocks := outboundmocks.NewMockStockRepository(t)
			publisher := outboundmocks.NewMockEventPublisher(t)
			svc := NewCatalogService(repo, stocks, publisher, newStubProvider(repo, stocks, publisher), "product-svc")

			tt.setupMocks(repo)

			result, err := svc.ListProducts(context.Background(), tt.params)
			if tt.errContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
				require.Nil(t, result)
				return
			}

			require.NoError(t, err)
			require.Len(t, result, tt.expectCount)
		})
	}
}

func TestUpdateProduct(t *testing.T) {
	productID := uuid.New()
	current := domain.Product{ID: productID, SKU: "SKU-1", Name: "Old", Price: 1000, Status: domain.ProductStatusDraft}
	updated := domain.Product{ID: productID, SKU: "SKU-2", Name: "New", Price: 2000, Status: domain.ProductStatusPublished}
	stock := domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 10, Reserved: 0, Available: 10}

	newSKU := "SKU-2"
	newName := "New"
	newPrice := int64(2000)
	newStatus := domain.ProductStatusPublished

	tests := []struct {
		name        string
		input       UpdateProductInput
		setupMocks  func(*outboundmocks.MockProductRepository, *outboundmocks.MockStockRepository)
		errIs       error
		errContains string
	}{
		{
			name:  "success",
			input: UpdateProductInput{ID: productID, SKU: &newSKU, Name: &newName, Price: &newPrice, Status: &newStatus},
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository) {
				products.EXPECT().GetByID(testifymock.Anything, productID).Return(current, nil)
				products.EXPECT().Update(testifymock.Anything, testifymock.Anything).Return(updated, nil)
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(stock, nil)
			},
		},
		{
			name:  "invalid input",
			input: UpdateProductInput{},
			errIs: ErrInvalidUpdateProductInput,
		},
		{
			name:  "not found",
			input: UpdateProductInput{ID: productID, SKU: &newSKU},
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository) {
				products.EXPECT().GetByID(testifymock.Anything, productID).Return(domain.Product{}, outbound.ErrProductNotFound)
			},
			errIs: outbound.ErrProductNotFound,
		},
		{
			name:  "update error",
			input: UpdateProductInput{ID: productID, SKU: &newSKU},
			setupMocks: func(products *outboundmocks.MockProductRepository, stocks *outboundmocks.MockStockRepository) {
				products.EXPECT().GetByID(testifymock.Anything, productID).Return(current, nil)
				products.EXPECT().Update(testifymock.Anything, testifymock.Anything).Return(domain.Product{}, errors.New("write failed"))
			},
			errContains: "update product",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := outboundmocks.NewMockProductRepository(t)
			stocks := outboundmocks.NewMockStockRepository(t)
			publisher := outboundmocks.NewMockEventPublisher(t)
			svc := NewCatalogService(products, stocks, publisher, newStubProvider(products, stocks, publisher), "product-svc")

			if tt.setupMocks != nil {
				tt.setupMocks(products, stocks)
			}

			result, err := svc.UpdateProduct(context.Background(), tt.input)
			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				require.Zero(t, result)
				return
			}

			require.NoError(t, err)
			require.Equal(t, "SKU-2", result.Product.SKU)
			require.Equal(t, int32(10), result.Stock.Quantity)
		})
	}
}

func TestReserveStock(t *testing.T) {
	productID := uuid.New()
	stock := domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 10, Reserved: 3, Available: 7}
	updated := domain.StockRecord{StockRecordID: stock.StockRecordID, ProductID: productID, Quantity: 10, Reserved: 5, Available: 5}

	tests := []struct {
		name          string
		input         ReserveStockInput
		setupMocks    func(*outboundmocks.MockStockRepository)
		assertNoCalls func(t *testing.T, stocks *outboundmocks.MockStockRepository)
		errIs         error
		errContains   string
	}{
		{
			name:  "success",
			input: ReserveStockInput{ProductID: productID, Quantity: 2},
			setupMocks: func(stocks *outboundmocks.MockStockRepository) {
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(stock, nil)
				stocks.EXPECT().Update(testifymock.Anything, testifymock.MatchedBy(func(got domain.StockRecord) bool {
					return got.StockRecordID == stock.StockRecordID && got.Reserved == 5
				})).Return(updated, nil)
			},
		},
		{
			name:  "invalid input",
			input: ReserveStockInput{},
			errIs: ErrInvalidStockInput,
		},
		{
			name:  "insufficient stock",
			input: ReserveStockInput{ProductID: productID, Quantity: 8},
			setupMocks: func(stocks *outboundmocks.MockStockRepository) {
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(stock, nil)
			},
			assertNoCalls: func(t *testing.T, stocks *outboundmocks.MockStockRepository) {
				stocks.AssertNotCalled(t, "Update", testifymock.Anything, testifymock.Anything)
			},
			errIs: ErrInsufficientStock,
		},
		{
			name:  "repo error",
			input: ReserveStockInput{ProductID: productID, Quantity: 2},
			setupMocks: func(stocks *outboundmocks.MockStockRepository) {
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(domain.StockRecord{}, errors.New("db down"))
			},
			errContains: "get stock by product id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := outboundmocks.NewMockProductRepository(t)
			stocks := outboundmocks.NewMockStockRepository(t)
			publisher := outboundmocks.NewMockEventPublisher(t)
			svc := NewCatalogService(products, stocks, publisher, newStubProvider(products, stocks, publisher), "product-svc")

			if tt.setupMocks != nil {
				tt.setupMocks(stocks)
			}

			result, err := svc.ReserveStock(context.Background(), tt.input)
			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.assertNoCalls != nil {
					tt.assertNoCalls(t, stocks)
				}
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				require.Zero(t, result)
				return
			}

			require.NoError(t, err)
			require.Equal(t, int32(5), result.Stock.Reserved)
		})
	}
}

func TestCreateProductStatusUnknownDefaultsToDraft(t *testing.T) {
	currencyID := uuid.New()
	productID := uuid.New()

	products := outboundmocks.NewMockProductRepository(t)
	stocks := outboundmocks.NewMockStockRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)
	svc := NewCatalogService(products, stocks, publisher, newStubProvider(products, stocks, publisher), "product-svc")

	products.EXPECT().
		Create(testifymock.Anything, testifymock.MatchedBy(func(product domain.Product) bool {
			return product.Status == domain.ProductStatusDraft
		})).
		Return(domain.Product{ID: productID, SKU: "SKU-1", Name: "Product", Price: 100, CurrencyID: currencyID, Status: domain.ProductStatusDraft}, nil)
	stocks.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 1, Reserved: 0, Available: 1}, nil)
	publisher.EXPECT().
		Publish(testifymock.Anything, testifymock.Anything).
		Return(nil)

	result, err := svc.CreateProduct(context.Background(), CreateProductInput{
		Product: domain.Product{
			SKU:        " SKU-1 ",
			Name:       " Product ",
			Price:      100,
			CurrencyID: currencyID,
			Status:     domain.ProductStatusUnknown,
		},
		InitialQuantity: 1,
	})

	require.NoError(t, err)
	require.Equal(t, domain.ProductStatusDraft, result.Product.Status)
}

func TestCreateProductProducerPropagationAndDefaultFallback(t *testing.T) {
	currencyID := uuid.New()

	tests := []struct {
		name             string
		configured       string
		expectedProducer string
	}{
		{name: "explicit producer propagated", configured: "catalog-runtime", expectedProducer: "catalog-runtime"},
		{name: "empty producer falls back to default", configured: "   ", expectedProducer: "product-svc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			productID := uuid.New()

			products := outboundmocks.NewMockProductRepository(t)
			stocks := outboundmocks.NewMockStockRepository(t)
			publisher := outboundmocks.NewMockEventPublisher(t)
			svc := NewCatalogService(products, stocks, publisher, newStubProvider(products, stocks, publisher), tt.configured)

			products.EXPECT().
				Create(testifymock.Anything, testifymock.Anything).
				Return(domain.Product{ID: productID, SKU: "SKU-1", Name: "Product", Price: 100, CurrencyID: currencyID, Status: domain.ProductStatusDraft}, nil)

			stocks.EXPECT().
				Create(testifymock.Anything, testifymock.Anything).
				Return(domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 1, Reserved: 0, Available: 1}, nil)

			publisher.EXPECT().
				Publish(testifymock.Anything, testifymock.MatchedBy(func(event domain.DomainEvent) bool {
					return event.Producer == tt.expectedProducer && event.EventName == "catalog.product.created"
				})).
				Return(nil)

			_, err := svc.CreateProduct(context.Background(), CreateProductInput{
				Product: domain.Product{
					SKU:        "SKU-1",
					Name:       "Product",
					Price:      100,
					CurrencyID: currencyID,
					Status:     domain.ProductStatusDraft,
				},
				InitialQuantity: 1,
			})

			require.NoError(t, err)
		})
	}
}

func TestCreateProductTxProviderError(t *testing.T) {
	products := outboundmocks.NewMockProductRepository(t)
	stocks := outboundmocks.NewMockStockRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)
	provider := newStubProvider(products, stocks, publisher)
	txErr := errors.New("tx unavailable")
	provider.err = txErr

	svc := NewCatalogService(products, stocks, publisher, provider, "product-svc")

	_, err := svc.CreateProduct(context.Background(), CreateProductInput{
		Product: domain.Product{
			SKU:        "SKU-1",
			Name:       "Product",
			Price:      100,
			CurrencyID: uuid.New(),
			Status:     domain.ProductStatusDraft,
		},
		InitialQuantity: 1,
	})

	require.ErrorIs(t, err, txErr)
	products.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	stocks.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	publisher.AssertNotCalled(t, "Publish", testifymock.Anything, testifymock.Anything)
}

func TestReleaseStock(t *testing.T) {
	productID := uuid.New()
	stock := domain.StockRecord{StockRecordID: uuid.New(), ProductID: productID, Quantity: 10, Reserved: 4, Available: 6}
	updated := domain.StockRecord{StockRecordID: stock.StockRecordID, ProductID: productID, Quantity: 10, Reserved: 2, Available: 8}

	tests := []struct {
		name        string
		input       ReleaseStockInput
		setupMocks  func(*outboundmocks.MockStockRepository)
		errIs       error
		errContains string
	}{
		{
			name:  "success",
			input: ReleaseStockInput{ProductID: productID, Quantity: 2},
			setupMocks: func(stocks *outboundmocks.MockStockRepository) {
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(stock, nil)
				stocks.EXPECT().Update(testifymock.Anything, testifymock.MatchedBy(func(got domain.StockRecord) bool {
					return got.StockRecordID == stock.StockRecordID && got.Reserved == 2
				})).Return(updated, nil)
			},
		},
		{
			name:  "invalid input",
			input: ReleaseStockInput{},
			errIs: ErrInvalidStockInput,
		},
		{
			name:  "release exceeds reserved",
			input: ReleaseStockInput{ProductID: productID, Quantity: 5},
			setupMocks: func(stocks *outboundmocks.MockStockRepository) {
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(stock, nil)
			},
			errIs: outbound.ErrInvalidStockUpdate,
		},
		{
			name:  "update repo error",
			input: ReleaseStockInput{ProductID: productID, Quantity: 2},
			setupMocks: func(stocks *outboundmocks.MockStockRepository) {
				stocks.EXPECT().GetByProductID(testifymock.Anything, productID).Return(stock, nil)
				stocks.EXPECT().Update(testifymock.Anything, testifymock.Anything).Return(domain.StockRecord{}, errors.New("update failed"))
			},
			errContains: "update stock record",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := outboundmocks.NewMockProductRepository(t)
			stocks := outboundmocks.NewMockStockRepository(t)
			publisher := outboundmocks.NewMockEventPublisher(t)
			svc := NewCatalogService(products, stocks, publisher, newStubProvider(products, stocks, publisher), "product-svc")

			if tt.setupMocks != nil {
				tt.setupMocks(stocks)
			}

			result, err := svc.ReleaseStock(context.Background(), tt.input)
			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				require.Zero(t, result)
				return
			}

			require.NoError(t, err)
			require.Equal(t, int32(2), result.Stock.Reserved)
		})
	}
}

type stubProvider struct {
	repos CatalogRepos
	err   error
}

func newStubProvider(
	products outbound.ProductRepository,
	stocks outbound.StockRepository,
	publisher outbound.EventPublisher,
) *stubProvider {
	return &stubProvider{repos: CatalogRepos{Products: products, Stocks: stocks, Publisher: publisher}}
}

func (p *stubProvider) WithTransaction(_ context.Context, _ *sql.TxOptions, fn func(uow tx.UnitOfWork[CatalogRepos]) error) error {
	if p.err != nil {
		return p.err
	}

	return fn(stubUnitOfWork{repos: p.repos})
}

type stubUnitOfWork struct {
	repos CatalogRepos
}

func (u stubUnitOfWork) Repos() CatalogRepos {
	return u.repos
}

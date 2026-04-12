package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
)

const (
	productEventsTopic      = "catalog.product.events"
	productCreatedEventName = "catalog.product.created"
	productAggregateType    = "product"
	eventSchemaVersion      = "1"
)

var (
	ErrInvalidCreateProductInput = errors.New("catalog create product input is invalid")
	ErrInvalidUpdateProductInput = errors.New("catalog update product input is invalid")
	ErrInvalidStockInput         = errors.New("catalog stock input is invalid")
	ErrInsufficientStock         = errors.New("catalog insufficient stock")
)

type CatalogRepos struct {
	Products  outbound.ProductRepository
	Stocks    outbound.StockRepository
	Publisher outbound.EventPublisher
}

type CatalogService struct {
	repos      CatalogRepos
	txProvider tx.Provider[CatalogRepos]
	producer   string
}

func NewCatalogService(
	products outbound.ProductRepository,
	stocks outbound.StockRepository,
	publisher outbound.EventPublisher,
	txProvider tx.Provider[CatalogRepos],
	producer string,
) *CatalogService {
	resolvedProducer := strings.TrimSpace(producer)
	if resolvedProducer == "" {
		resolvedProducer = "product-svc"
	}

	return &CatalogService{
		repos: CatalogRepos{
			Products:  products,
			Stocks:    stocks,
			Publisher: publisher,
		},
		txProvider: txProvider,
		producer:   resolvedProducer,
	}
}

type CreateProductInput struct {
	Product         domain.Product
	InitialQuantity int32
}

type CreateProductResult struct {
	Product domain.Product
	Stock   domain.StockRecord
}

type GetProductResult struct {
	Product domain.Product
	Stock   domain.StockRecord
}

type UpdateProductInput struct {
	ID          uuid.UUID
	SKU         *string
	Name        *string
	Description *string
	Price       *int64
	Status      *domain.ProductStatus
}

type UpdateProductResult = GetProductResult

type ReserveStockInput struct {
	ProductID uuid.UUID
	Quantity  int32
}

type ReserveStockResult struct {
	Stock domain.StockRecord
}

type ReleaseStockInput = ReserveStockInput

type ReleaseStockResult = ReserveStockResult

func (s *CatalogService) CreateProduct(ctx context.Context, input CreateProductInput) (CreateProductResult, error) {
	normalizedProduct, err := normalizeCreateProductInput(input.Product)
	if err != nil {
		return CreateProductResult{}, ErrInvalidCreateProductInput
	}

	var result CreateProductResult
	err = s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[CatalogRepos]) error {
		repos := uow.Repos()

		createdProduct, err := repos.Products.Create(ctx, normalizedProduct)
		if err != nil {
			if errors.Is(err, outbound.ErrProductAlreadyExists) || errors.Is(err, outbound.ErrInvalidCurrency) {
				return err
			}

			return fmt.Errorf("create product: %w", err)
		}

		createdStock, err := repos.Stocks.Create(ctx, domain.StockRecord{
			ProductID: createdProduct.ID,
			Quantity:  input.InitialQuantity,
			Reserved:  0,
		})
		if err != nil {
			if errors.Is(err, outbound.ErrProductNotFound) || errors.Is(err, outbound.ErrInvalidStockUpdate) {
				return err
			}

			return fmt.Errorf("create stock record: %w", err)
		}

		event := newProductCreatedEvent(createdProduct, s.producer)
		if err := repos.Publisher.Publish(ctx, event); err != nil {
			return fmt.Errorf("publish product created event: %w", err)
		}

		result = CreateProductResult{Product: createdProduct, Stock: createdStock}

		return nil
	})
	if err != nil {
		return CreateProductResult{}, err
	}

	return result, nil
}

func (s *CatalogService) GetProduct(ctx context.Context, productID uuid.UUID) (GetProductResult, error) {
	if productID == uuid.Nil {
		return GetProductResult{}, outbound.ErrProductNotFound
	}

	product, err := s.repos.Products.GetByID(ctx, productID)
	if err != nil {
		if errors.Is(err, outbound.ErrProductNotFound) {
			return GetProductResult{}, outbound.ErrProductNotFound
		}

		return GetProductResult{}, fmt.Errorf("get product by id: %w", err)
	}

	stock, err := s.repos.Stocks.GetByProductID(ctx, productID)
	if err != nil {
		if errors.Is(err, outbound.ErrStockRecordNotFound) {
			return GetProductResult{}, outbound.ErrStockRecordNotFound
		}

		return GetProductResult{}, fmt.Errorf("get stock by product id: %w", err)
	}

	return GetProductResult{Product: product, Stock: stock}, nil
}

func (s *CatalogService) ListProducts(ctx context.Context, params outbound.ProductListParams) ([]domain.Product, error) {
	products, err := s.repos.Products.List(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}

	return products, nil
}

func (s *CatalogService) UpdateProduct(ctx context.Context, input UpdateProductInput) (UpdateProductResult, error) {
	if err := validateUpdateProductInput(input); err != nil {
		return UpdateProductResult{}, ErrInvalidUpdateProductInput
	}

	currentProduct, err := s.repos.Products.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, outbound.ErrProductNotFound) {
			return UpdateProductResult{}, outbound.ErrProductNotFound
		}

		return UpdateProductResult{}, fmt.Errorf("get product by id: %w", err)
	}

	updatedProduct := mergeProductUpdate(currentProduct, normalizeUpdateInput(input))

	product, err := s.repos.Products.Update(ctx, updatedProduct)
	if err != nil {
		if errors.Is(err, outbound.ErrProductNotFound) || errors.Is(err, outbound.ErrInvalidCurrency) {
			return UpdateProductResult{}, err
		}

		return UpdateProductResult{}, fmt.Errorf("update product: %w", err)
	}

	stock, err := s.repos.Stocks.GetByProductID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, outbound.ErrStockRecordNotFound) {
			return UpdateProductResult{}, outbound.ErrStockRecordNotFound
		}

		return UpdateProductResult{}, fmt.Errorf("get stock by product id: %w", err)
	}

	return UpdateProductResult{Product: product, Stock: stock}, nil
}

func (s *CatalogService) ReserveStock(ctx context.Context, input ReserveStockInput) (ReserveStockResult, error) {
	if input.ProductID == uuid.Nil || input.Quantity <= 0 {
		return ReserveStockResult{}, ErrInvalidStockInput
	}

	var result ReserveStockResult
	err := s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[CatalogRepos]) error {
		repos := uow.Repos()

		stock, err := repos.Stocks.GetByProductID(ctx, input.ProductID)
		if err != nil {
			if errors.Is(err, outbound.ErrStockRecordNotFound) {
				return outbound.ErrStockRecordNotFound
			}

			return fmt.Errorf("get stock by product id: %w", err)
		}

		if stock.Available < input.Quantity {
			return ErrInsufficientStock
		}

		stock.Reserved += input.Quantity

		updated, err := repos.Stocks.Update(ctx, stock)
		if err != nil {
			if errors.Is(err, outbound.ErrStockRecordNotFound) || errors.Is(err, outbound.ErrInvalidStockUpdate) {
				return err
			}

			return fmt.Errorf("update stock record: %w", err)
		}

		result = ReserveStockResult{Stock: updated}

		return nil
	})
	if err != nil {
		return ReserveStockResult{}, err
	}

	return result, nil
}

func (s *CatalogService) ReleaseStock(ctx context.Context, input ReleaseStockInput) (ReleaseStockResult, error) {
	if input.ProductID == uuid.Nil || input.Quantity <= 0 {
		return ReleaseStockResult{}, ErrInvalidStockInput
	}

	var result ReleaseStockResult
	err := s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[CatalogRepos]) error {
		repos := uow.Repos()

		stock, err := repos.Stocks.GetByProductID(ctx, input.ProductID)
		if err != nil {
			if errors.Is(err, outbound.ErrStockRecordNotFound) {
				return outbound.ErrStockRecordNotFound
			}

			return fmt.Errorf("get stock by product id: %w", err)
		}

		if input.Quantity > stock.Reserved {
			return outbound.ErrInvalidStockUpdate
		}

		stock.Reserved -= input.Quantity

		updated, err := repos.Stocks.Update(ctx, stock)
		if err != nil {
			if errors.Is(err, outbound.ErrStockRecordNotFound) || errors.Is(err, outbound.ErrInvalidStockUpdate) {
				return err
			}

			return fmt.Errorf("update stock record: %w", err)
		}

		result = ReleaseStockResult{Stock: updated}

		return nil
	})
	if err != nil {
		return ReleaseStockResult{}, err
	}

	return result, nil
}

func mergeProductUpdate(product domain.Product, input UpdateProductInput) domain.Product {
	if input.SKU != nil {
		product.SKU = *input.SKU
	}

	if input.Name != nil {
		product.Name = *input.Name
	}

	if input.Description != nil {
		product.Description = *input.Description
	}

	if input.Price != nil {
		product.Price = *input.Price
	}

	if input.Status != nil {
		product.Status = *input.Status
	}

	return product
}

func normalizeCreateProductInput(product domain.Product) (domain.Product, error) {
	product.SKU = strings.TrimSpace(product.SKU)
	product.Name = strings.TrimSpace(product.Name)

	if product.Status == domain.ProductStatusUnknown {
		product.Status = domain.ProductStatusDraft
	}

	if product.SKU == "" || product.Name == "" || product.CurrencyID == uuid.Nil || product.Price < 0 || !isValidProductStatus(product.Status) {
		return domain.Product{}, ErrInvalidCreateProductInput
	}

	return product, nil
}

func validateUpdateProductInput(input UpdateProductInput) error {
	if input.ID == uuid.Nil {
		return ErrInvalidUpdateProductInput
	}

	if input.SKU != nil && strings.TrimSpace(*input.SKU) == "" {
		return ErrInvalidUpdateProductInput
	}

	if input.Name != nil && strings.TrimSpace(*input.Name) == "" {
		return ErrInvalidUpdateProductInput
	}

	if input.Price != nil && *input.Price < 0 {
		return ErrInvalidUpdateProductInput
	}

	if input.Status != nil {
		status := *input.Status
		if status != domain.ProductStatusUnknown && !isValidProductStatus(status) {
			return ErrInvalidUpdateProductInput
		}
	}

	return nil
}

func normalizeUpdateInput(input UpdateProductInput) UpdateProductInput {
	if input.SKU != nil {
		trimmed := strings.TrimSpace(*input.SKU)
		input.SKU = &trimmed
	}

	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		input.Name = &trimmed
	}

	if input.Status != nil && *input.Status == domain.ProductStatusUnknown {
		status := domain.ProductStatusDraft
		input.Status = &status
	}

	return input
}

func isValidProductStatus(status domain.ProductStatus) bool {
	switch status {
	case domain.ProductStatusDraft, domain.ProductStatusPublished, domain.ProductStatusArchived:
		return true
	default:
		return false
	}
}

func newProductCreatedEvent(product domain.Product, producer string) domain.DomainEvent {
	eventID := uuid.NewString()
	occurredAt := time.Now().UTC()

	return domain.DomainEvent{
		EventID:       eventID,
		EventName:     productCreatedEventName,
		Producer:      producer,
		OccurredAt:    occurredAt,
		CorrelationID: eventID,
		CausationID:   eventID,
		SchemaVersion: eventSchemaVersion,
		AggregateType: productAggregateType,
		AggregateID:   product.ID.String(),
		Topic:         productEventsTopic,
		Key:           product.ID.String(),
		Payload: domain.ProductCreatedPayload{
			ProductID:  product.ID.String(),
			SKU:        product.SKU,
			Name:       product.Name,
			Status:     product.Status,
			Price:      product.Price,
			Currency:   product.Currency,
			CategoryID: categoryIDString(product.CategoryID),
		},
		Headers: map[string]string{},
	}
}

func categoryIDString(categoryID *uuid.UUID) string {
	if categoryID == nil {
		return ""
	}

	return categoryID.String()
}

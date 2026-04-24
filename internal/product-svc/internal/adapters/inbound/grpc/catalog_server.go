package grpc

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type catalogService interface {
	GetProduct(ctx context.Context, productID uuid.UUID) (catalog.GetProductResult, error)
	GetProductBySKU(ctx context.Context, sku string) (catalog.GetProductResult, error)
	ListProducts(ctx context.Context, params outbound.ProductListParams) ([]domain.Product, error)
	ReserveStock(ctx context.Context, input catalog.ReserveStockInput) (catalog.ReserveStockResult, error)
	ReleaseStock(ctx context.Context, input catalog.ReleaseStockInput) (catalog.ReleaseStockResult, error)
}

type CatalogServer struct {
	catalogv1.UnimplementedCatalogServiceServer

	service         catalogService
	logger          *slog.Logger
	defaultPageSize int32
}

func NewCatalogServer(service catalogService, logger *slog.Logger, defaultPageSize int32) *CatalogServer {
	if logger == nil {
		logger = slog.Default()
	}
	if defaultPageSize <= 0 {
		defaultPageSize = defaultListPageSize
	}

	return &CatalogServer{service: service, logger: logger, defaultPageSize: defaultPageSize}
}

func (s *CatalogServer) GetProduct(ctx context.Context, req *catalogv1.GetProductRequest) (*catalogv1.GetProductResponse, error) {
	productID, err := toProductID(req.GetProductId())
	if err != nil {
		return nil, err
	}

	result, err := s.service.GetProduct(ctx, productID)
	if err != nil {
		return nil, mapServiceError(err)
	}

	return &catalogv1.GetProductResponse{Product: toProtoProduct(result.Product)}, nil
}

func (s *CatalogServer) GetProductBySKU(ctx context.Context, req *catalogv1.GetProductBySKURequest) (*catalogv1.GetProductBySKUResponse, error) {
	sku, err := toSKU(req.GetSku())
	if err != nil {
		return nil, err
	}

	result, err := s.service.GetProductBySKU(ctx, sku)
	if err != nil {
		return nil, mapServiceError(err)
	}

	return &catalogv1.GetProductBySKUResponse{Product: toProtoProduct(result.Product)}, nil
}

func (s *CatalogServer) ListPublishedProducts(ctx context.Context, req *catalogv1.ListPublishedProductsRequest) (*catalogv1.ListPublishedProductsResponse, error) {
	params, err := toListParams(req, s.defaultPageSize)
	if err != nil {
		return nil, err
	}

	var categoryID string
	if trimmed := req.GetCategoryId(); trimmed != "" {
		parsedCategoryID, err := toProductID(trimmed)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid category id")
		}
		categoryID = parsedCategoryID.String()
	}

	products, err := s.service.ListProducts(ctx, params)
	if err != nil {
		return nil, mapServiceError(err)
	}

	items := make([]*catalogv1.Product, 0, params.Limit)
	for _, product := range products {
		if product.Status != domain.ProductStatusPublished {
			continue
		}

		if categoryID != "" {
			if product.CategoryID == nil || product.CategoryID.String() != categoryID {
				continue
			}
		}

		items = append(items, toProtoProduct(product))
		if int32(len(items)) == params.Limit {
			break
		}
	}

	return &catalogv1.ListPublishedProductsResponse{
		Items: items,
		Page:  &commonv1.PageResponse{},
	}, nil
}

func (s *CatalogServer) ReserveStock(ctx context.Context, req *catalogv1.ReserveStockRequest) (*catalogv1.ReserveStockResponse, error) {
	orderID, err := toOrderID(req.GetOrderId())
	if err != nil {
		return nil, err
	}

	if len(req.GetItems()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "items are required")
	}

	for _, item := range req.GetItems() {
		productID, err := toProductID(item.GetProductId())
		if err != nil {
			return nil, err
		}

		quantity, err := toReserveQuantity(item.GetQuantity())
		if err != nil {
			return nil, err
		}

		if _, err := s.service.ReserveStock(ctx, catalog.ReserveStockInput{OrderID: orderID, ProductID: productID, Quantity: quantity}); err != nil {
			return nil, mapServiceError(err)
		}
	}

	return &catalogv1.ReserveStockResponse{Accepted: true}, nil
}

func toOrderID(raw string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return uuid.Nil, status.Errorf(codes.InvalidArgument, "invalid order id")
	}

	orderID, err := uuid.Parse(trimmed)
	if err != nil || orderID == uuid.Nil {
		return uuid.Nil, status.Errorf(codes.InvalidArgument, "invalid order id")
	}

	return orderID, nil
}

func (s *CatalogServer) ReleaseStock(ctx context.Context, req *catalogv1.ReleaseStockRequest) (*catalogv1.ReleaseStockResponse, error) {
	orderID, err := toOrderID(req.GetOrderId())
	if err != nil {
		return nil, err
	}

	if _, err := s.service.ReleaseStock(ctx, catalog.ReleaseStockInput{OrderID: orderID}); err != nil {
		return nil, mapServiceError(err)
	}

	return &catalogv1.ReleaseStockResponse{Accepted: true}, nil
}

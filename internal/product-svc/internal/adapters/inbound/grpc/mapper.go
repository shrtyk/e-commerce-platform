package grpc

import (
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
)

const defaultListPageSize int32 = 100

func toProductID(raw string) (uuid.UUID, error) {
	productID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, status.Errorf(codes.InvalidArgument, "invalid product id")
	}

	if productID == uuid.Nil {
		return uuid.Nil, status.Errorf(codes.InvalidArgument, "invalid product id")
	}

	return productID, nil
}

func toSKU(raw string) (string, error) {
	sku := strings.TrimSpace(raw)
	if sku == "" {
		return "", status.Errorf(codes.InvalidArgument, "invalid sku")
	}

	return sku, nil
}

func toReserveQuantity(quantity int64) (int32, error) {
	if quantity <= 0 {
		return 0, status.Errorf(codes.InvalidArgument, "quantity must be positive")
	}

	if quantity > int64(int32(^uint32(0)>>1)) {
		return 0, status.Errorf(codes.InvalidArgument, "quantity is out of range")
	}

	return int32(quantity), nil
}

func toListParams(req *catalogv1.ListPublishedProductsRequest, defaultLimit int32) (outbound.ProductListParams, error) {
	if defaultLimit <= 0 {
		defaultLimit = defaultListPageSize
	}

	params := outbound.ProductListParams{Limit: defaultLimit}
	if req == nil || req.GetPage() == nil {
		return params, nil
	}

	page := req.GetPage()
	if page.GetPageSize() > 0 {
		if page.GetPageSize() > uint32(int32(^uint32(0)>>1)) {
			return outbound.ProductListParams{}, status.Errorf(codes.InvalidArgument, "page size is out of range")
		}
		params.Limit = int32(page.GetPageSize())
	}

	if token := strings.TrimSpace(page.GetPageToken()); token != "" {
		offset, err := strconv.ParseInt(token, 10, 32)
		if err != nil || offset < 0 {
			return outbound.ProductListParams{}, status.Errorf(codes.InvalidArgument, "invalid page token")
		}
		params.Offset = int32(offset)
	}

	return params, nil
}

func toProtoProduct(product domain.Product) *catalogv1.Product {
	protoProduct := &catalogv1.Product{
		ProductId: product.ID.String(),
		Sku:       product.SKU,
		Name:      product.Name,
		Status:    toProtoStatus(product.Status),
		Price: &commonv1.Money{
			Amount:   product.Price,
			Currency: product.Currency,
		},
	}

	if product.Description != "" {
		protoProduct.Description = product.Description
	}

	if product.CategoryID != nil {
		protoProduct.CategoryId = product.CategoryID.String()
	}

	return protoProduct
}

func toProtoStatus(statusValue domain.ProductStatus) catalogv1.ProductStatus {
	switch statusValue {
	case domain.ProductStatusDraft:
		return catalogv1.ProductStatus_PRODUCT_STATUS_DRAFT
	case domain.ProductStatusPublished:
		return catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED
	case domain.ProductStatusArchived:
		return catalogv1.ProductStatus_PRODUCT_STATUS_ARCHIVED
	default:
		return catalogv1.ProductStatus_PRODUCT_STATUS_UNSPECIFIED
	}
}

func mapServiceError(err error) error {
	switch {
	case errors.Is(err, outbound.ErrProductNotFound), errors.Is(err, outbound.ErrStockRecordNotFound):
		return status.Errorf(codes.NotFound, "resource not found")
	case errors.Is(err, catalog.ErrInvalidStockInput),
		errors.Is(err, catalog.ErrInvalidCreateProductInput),
		errors.Is(err, catalog.ErrInvalidUpdateProductInput),
		errors.Is(err, outbound.ErrInvalidStockUpdate):
		return status.Errorf(codes.InvalidArgument, "invalid request")
	case errors.Is(err, catalog.ErrInsufficientStock):
		return status.Errorf(codes.FailedPrecondition, "insufficient stock")
	default:
		return status.Errorf(codes.Internal, "internal error")
	}
}

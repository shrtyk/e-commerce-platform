package grpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CatalogReader struct {
	client catalogv1.CatalogServiceClient
}

func NewCatalogReader(client catalogv1.CatalogServiceClient) *CatalogReader {
	return &CatalogReader{client: client}
}

func (r *CatalogReader) GetProductBySKU(ctx context.Context, sku string) (outbound.CatalogProduct, error) {
	response, err := r.client.GetProductBySKU(withPropagationMetadata(ctx), &catalogv1.GetProductBySKURequest{Sku: sku})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return outbound.CatalogProduct{}, outbound.ErrProductNotFound
		}

		return outbound.CatalogProduct{}, fmt.Errorf("catalog get product by sku: %w", err)
	}

	if response == nil || response.GetProduct() == nil || response.GetProduct().GetPrice() == nil {
		return outbound.CatalogProduct{}, fmt.Errorf("catalog get product by sku: %w", errors.New("invalid catalog response"))
	}

	product := response.GetProduct()
	price := product.GetPrice()
	if price.GetCurrency() == "" {
		return outbound.CatalogProduct{}, fmt.Errorf("catalog get product by sku: %w", errors.New("invalid catalog response"))
	}
	if price.GetAmount() < 0 {
		return outbound.CatalogProduct{}, fmt.Errorf("catalog get product by sku: %w", errors.New("invalid catalog response"))
	}

	return outbound.CatalogProduct{
		ProductID:   product.GetProductId(),
		SKU:         product.GetSku(),
		Name:        product.GetName(),
		Price:       price.GetAmount(),
		Currency:    price.GetCurrency(),
		IsPublished: product.GetStatus() == catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED,
	}, nil
}

func withPropagationMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md = md.Copy()
	}

	md = observability.InjectGRPCMetadata(ctx, md)

	return metadata.NewOutgoingContext(ctx, md)
}

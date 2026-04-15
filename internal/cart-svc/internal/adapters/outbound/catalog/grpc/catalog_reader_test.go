package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
)

func TestCatalogReaderGetProductBySKU(t *testing.T) {
	tests := []struct {
		name        string
		client      *stubCatalogClient
		errIs       error
		errContains string
		assert      func(*testing.T, outbound.CatalogProduct, *stubCatalogClient)
	}{
		{
			name: "success",
			client: &stubCatalogClient{
				getProductBySKUFn: func(_ context.Context, req *catalogv1.GetProductBySKURequest, _ ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					require.Equal(t, "SKU-1", req.GetSku())
					return &catalogv1.GetProductBySKUResponse{Product: &catalogv1.Product{
						ProductId: "00000000-0000-0000-0000-000000000111",
						Sku:       "SKU-1",
						Name:      "Product 1",
						Status:    catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED,
						Price:     &commonv1.Money{Amount: 1000, Currency: "USD"},
					}}, nil
				},
			},
			assert: func(t *testing.T, product outbound.CatalogProduct, client *stubCatalogClient) {
				require.Equal(t, "SKU-1", product.SKU)
				require.Equal(t, int64(1000), product.Price)
				require.Equal(t, "USD", product.Currency)
				require.True(t, product.IsPublished)
				require.Equal(t, 1, client.getProductBySKUCalls)
			},
		},
		{
			name: "not found",
			client: &stubCatalogClient{
				getProductBySKUFn: func(_ context.Context, _ *catalogv1.GetProductBySKURequest, _ ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return nil, status.Error(codes.NotFound, "not found")
				},
			},
			errIs: outbound.ErrProductNotFound,
		},
		{
			name: "upstream failure",
			client: &stubCatalogClient{
				getProductBySKUFn: func(_ context.Context, _ *catalogv1.GetProductBySKURequest, _ ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return nil, status.Error(codes.Unavailable, "down")
				},
			},
			errContains: "catalog get product by sku",
		},
		{
			name: "invalid money nil price",
			client: &stubCatalogClient{
				getProductBySKUFn: func(_ context.Context, _ *catalogv1.GetProductBySKURequest, _ ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return &catalogv1.GetProductBySKUResponse{Product: &catalogv1.Product{ProductId: "00000000-0000-0000-0000-000000000111", Sku: "SKU-1", Name: "Product 1", Price: nil}}, nil
				},
			},
			errContains: "invalid catalog response",
		},
		{
			name: "invalid money empty currency",
			client: &stubCatalogClient{
				getProductBySKUFn: func(_ context.Context, _ *catalogv1.GetProductBySKURequest, _ ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return &catalogv1.GetProductBySKUResponse{Product: &catalogv1.Product{Price: &commonv1.Money{Amount: 100, Currency: ""}}}, nil
				},
			},
			errContains: "invalid catalog response",
		},
		{
			name: "invalid money negative amount",
			client: &stubCatalogClient{
				getProductBySKUFn: func(_ context.Context, _ *catalogv1.GetProductBySKURequest, _ ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return &catalogv1.GetProductBySKUResponse{Product: &catalogv1.Product{Price: &commonv1.Money{Amount: -1, Currency: "USD"}}}, nil
				},
			},
			errContains: "invalid catalog response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewCatalogReader(tt.client)
			product, err := reader.GetProductBySKU(context.Background(), "SKU-1")

			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				require.Zero(t, product)
				return
			}

			require.NoError(t, err)
			if tt.assert != nil {
				tt.assert(t, product, tt.client)
			}
		})
	}
}

type stubCatalogClient struct {
	getProductBySKUFn    func(ctx context.Context, in *catalogv1.GetProductBySKURequest, opts ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error)
	getProductBySKUCalls int
}

func (s *stubCatalogClient) GetProduct(_ context.Context, _ *catalogv1.GetProductRequest, _ ...grpc.CallOption) (*catalogv1.GetProductResponse, error) {
	return nil, errors.New("unexpected GetProduct call")
}

func (s *stubCatalogClient) GetProductBySKU(ctx context.Context, in *catalogv1.GetProductBySKURequest, opts ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
	if s.getProductBySKUFn == nil {
		return nil, errors.New("unexpected GetProductBySKU call")
	}

	s.getProductBySKUCalls++

	return s.getProductBySKUFn(ctx, in, opts...)
}

func (s *stubCatalogClient) ListPublishedProducts(_ context.Context, _ *catalogv1.ListPublishedProductsRequest, _ ...grpc.CallOption) (*catalogv1.ListPublishedProductsResponse, error) {
	return nil, errors.New("unexpected ListPublishedProducts call")
}

func (s *stubCatalogClient) ReserveStock(_ context.Context, _ *catalogv1.ReserveStockRequest, _ ...grpc.CallOption) (*catalogv1.ReserveStockResponse, error) {
	return nil, errors.New("unexpected ReserveStock call")
}

func (s *stubCatalogClient) ReleaseStock(_ context.Context, _ *catalogv1.ReleaseStockRequest, _ ...grpc.CallOption) (*catalogv1.ReleaseStockResponse, error) {
	return nil, errors.New("unexpected ReleaseStock call")
}

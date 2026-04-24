package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
)

func TestGetProduct(t *testing.T) {
	productID := uuid.New()

	tests := []struct {
		name         string
		request      *catalogv1.GetProductRequest
		setup        func(*stubCatalogService)
		expectedCode codes.Code
		assert       func(*testing.T, *catalogv1.GetProductResponse)
	}{
		{
			name:    "success",
			request: &catalogv1.GetProductRequest{ProductId: productID.String()},
			setup: func(svc *stubCatalogService) {
				svc.getProductResult = catalog.GetProductResult{Product: domain.Product{ID: productID, SKU: "SKU-1", Name: "Coffee", Price: 500, Currency: "USD", Status: domain.ProductStatusPublished}}
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *catalogv1.GetProductResponse) {
				require.Equal(t, "SKU-1", response.GetProduct().GetSku())
				require.Equal(t, catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED, response.GetProduct().GetStatus())
			},
		},
		{
			name:         "invalid id",
			request:      &catalogv1.GetProductRequest{ProductId: "bad-id"},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:    "not found",
			request: &catalogv1.GetProductRequest{ProductId: productID.String()},
			setup: func(svc *stubCatalogService) {
				svc.getProductErr = outbound.ErrProductNotFound
			},
			expectedCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubCatalogService{}
			if tt.setup != nil {
				tt.setup(svc)
			}

			server := NewCatalogServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
			response, err := server.GetProduct(context.Background(), tt.request)
			require.Equal(t, tt.expectedCode, status.Code(err))

			if tt.expectedCode != codes.OK {
				require.Nil(t, response)
				return
			}

			require.NotNil(t, response)
			if tt.assert != nil {
				tt.assert(t, response)
			}
		})
	}
}

func TestListPublishedProducts(t *testing.T) {
	tests := []struct {
		name         string
		request      *catalogv1.ListPublishedProductsRequest
		setup        func(*stubCatalogService)
		expectedCode codes.Code
		assert       func(*testing.T, *catalogv1.ListPublishedProductsResponse, *stubCatalogService)
	}{
		{
			name: "no next token when filtered page shorter",
			request: &catalogv1.ListPublishedProductsRequest{
				CategoryId: uuid.NewString(),
				Page:       &commonv1.PageRequest{PageSize: 3, PageToken: "2"},
			},
			setup: func(svc *stubCatalogService) {
				categoryID := uuid.MustParse(svc.listCategoryID)
				svc.listProductsResult = []domain.Product{
					{ID: uuid.New(), SKU: "A", Name: "A", Price: 10, Currency: "USD", Status: domain.ProductStatusPublished, CategoryID: &categoryID},
					{ID: uuid.New(), SKU: "B", Name: "B", Price: 10, Currency: "USD", Status: domain.ProductStatusDraft, CategoryID: &categoryID},
					{ID: uuid.New(), SKU: "C", Name: "C", Price: 10, Currency: "USD", Status: domain.ProductStatusPublished},
				}
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *catalogv1.ListPublishedProductsResponse, svc *stubCatalogService) {
				require.Len(t, response.GetItems(), 1)
				require.Equal(t, int32(3), svc.lastListParams.Limit)
				require.Equal(t, int32(2), svc.lastListParams.Offset)
				require.Equal(t, "", response.GetPage().GetNextPageToken())
			},
		},
		{
			name: "next token when page filled",
			request: &catalogv1.ListPublishedProductsRequest{
				Page: &commonv1.PageRequest{PageSize: 2},
			},
			setup: func(svc *stubCatalogService) {
				svc.listProductsResult = []domain.Product{
					{ID: uuid.New(), SKU: "A", Name: "A", Price: 10, Currency: "USD", Status: domain.ProductStatusPublished},
					{ID: uuid.New(), SKU: "B", Name: "B", Price: 10, Currency: "USD", Status: domain.ProductStatusPublished},
					{ID: uuid.New(), SKU: "C", Name: "C", Price: 10, Currency: "USD", Status: domain.ProductStatusPublished},
				}
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *catalogv1.ListPublishedProductsResponse, _ *stubCatalogService) {
				require.Len(t, response.GetItems(), 2)
				require.Equal(t, "", response.GetPage().GetNextPageToken())
			},
		},
		{
			name:         "invalid page token",
			request:      &catalogv1.ListPublishedProductsRequest{Page: &commonv1.PageRequest{PageToken: "bad"}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "oversized page size",
			request:      &catalogv1.ListPublishedProductsRequest{Page: &commonv1.PageRequest{PageSize: math.MaxUint32}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid category id",
			request:      &catalogv1.ListPublishedProductsRequest{CategoryId: "bad-id"},
			expectedCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubCatalogService{listCategoryID: tt.request.GetCategoryId()}
			if tt.setup != nil {
				tt.setup(svc)
			}

			server := NewCatalogServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
			response, err := server.ListPublishedProducts(context.Background(), tt.request)
			require.Equal(t, tt.expectedCode, status.Code(err))

			if tt.expectedCode != codes.OK {
				require.Nil(t, response)
				return
			}

			require.NotNil(t, response)
			if tt.assert != nil {
				tt.assert(t, response, svc)
			}
		})
	}
}

func TestListPublishedProductsUsesConfiguredDefaultPageSize(t *testing.T) {
	svc := &stubCatalogService{}
	server := NewCatalogServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), 55)

	response, err := server.ListPublishedProducts(context.Background(), &catalogv1.ListPublishedProductsRequest{})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, int32(55), svc.lastListParams.Limit)
}

func TestReserveStock(t *testing.T) {
	productID1 := uuid.New()
	productID2 := uuid.New()

	tests := []struct {
		name         string
		request      *catalogv1.ReserveStockRequest
		setup        func(*stubCatalogService)
		expectedCode codes.Code
		assert       func(*testing.T, *stubCatalogService)
	}{
		{
			name:         "empty items",
			request:      &catalogv1.ReserveStockRequest{OrderId: uuid.NewString()},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid order id empty",
			request:      &catalogv1.ReserveStockRequest{OrderId: "", Items: []*catalogv1.ReservationItem{{ProductId: productID1.String(), Quantity: 1}}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid order id format",
			request:      &catalogv1.ReserveStockRequest{OrderId: "bad-order", Items: []*catalogv1.ReservationItem{{ProductId: productID1.String(), Quantity: 1}}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid product id",
			request:      &catalogv1.ReserveStockRequest{OrderId: uuid.NewString(), Items: []*catalogv1.ReservationItem{{ProductId: "bad-id", Quantity: 1}}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid quantity zero",
			request:      &catalogv1.ReserveStockRequest{OrderId: uuid.NewString(), Items: []*catalogv1.ReservationItem{{ProductId: productID1.String(), Quantity: 0}}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid quantity out of range",
			request:      &catalogv1.ReserveStockRequest{OrderId: uuid.NewString(), Items: []*catalogv1.ReservationItem{{ProductId: productID1.String(), Quantity: int64(math.MaxInt32) + 1}}},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "success",
			request: &catalogv1.ReserveStockRequest{OrderId: uuid.NewString(), Items: []*catalogv1.ReservationItem{
				{ProductId: productID1.String(), Quantity: 2},
				{ProductId: productID2.String(), Quantity: 1},
			}},
			expectedCode: codes.OK,
			assert: func(t *testing.T, svc *stubCatalogService) {
				require.Len(t, svc.reserveInputs, 2)
				require.NotEqual(t, uuid.Nil, svc.reserveInputs[0].OrderID)
				require.Equal(t, svc.reserveInputs[0].OrderID, svc.reserveInputs[1].OrderID)
				require.Equal(t, int32(2), svc.reserveInputs[0].Quantity)
			},
		},
		{
			name: "fail fast",
			request: &catalogv1.ReserveStockRequest{OrderId: uuid.NewString(), Items: []*catalogv1.ReservationItem{
				{ProductId: productID1.String(), Quantity: 10},
				{ProductId: productID2.String(), Quantity: 10},
			}},
			setup: func(svc *stubCatalogService) {
				svc.reserveStockErrOnCall = map[int]error{0: catalog.ErrInsufficientStock}
			},
			expectedCode: codes.FailedPrecondition,
			assert: func(t *testing.T, svc *stubCatalogService) {
				require.Len(t, svc.reserveInputs, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubCatalogService{}
			if tt.setup != nil {
				tt.setup(svc)
			}

			server := NewCatalogServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
			response, err := server.ReserveStock(context.Background(), tt.request)
			require.Equal(t, tt.expectedCode, status.Code(err))

			if tt.expectedCode != codes.OK {
				require.Nil(t, response)
			} else {
				require.True(t, response.GetAccepted())
			}

			if tt.assert != nil {
				tt.assert(t, svc)
			}
		})
	}
}

func TestReleaseStock(t *testing.T) {
	orderID := uuid.New()

	tests := []struct {
		name         string
		request      *catalogv1.ReleaseStockRequest
		setup        func(*stubCatalogService)
		expectedCode codes.Code
		assert       func(*testing.T, *stubCatalogService, *catalogv1.ReleaseStockResponse)
	}{
		{
			name:         "invalid order id",
			request:      &catalogv1.ReleaseStockRequest{OrderId: "bad-order"},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "success",
			request:      &catalogv1.ReleaseStockRequest{OrderId: orderID.String()},
			expectedCode: codes.OK,
			assert: func(t *testing.T, svc *stubCatalogService, response *catalogv1.ReleaseStockResponse) {
				require.True(t, response.GetAccepted())
				require.Len(t, svc.releaseInputs, 1)
				require.Equal(t, orderID, svc.releaseInputs[0].OrderID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubCatalogService{}
			if tt.setup != nil {
				tt.setup(svc)
			}

			server := NewCatalogServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
			response, err := server.ReleaseStock(context.Background(), tt.request)
			require.Equal(t, tt.expectedCode, status.Code(err))

			if tt.expectedCode != codes.OK {
				require.Nil(t, response)
				return
			}

			if tt.assert != nil {
				tt.assert(t, svc, response)
			}
		})
	}
}

type stubCatalogService struct {
	getProductResult catalog.GetProductResult
	getProductErr    error
	getBySKUResult   catalog.GetProductResult
	getBySKUErr      error

	listCategoryID     string
	listProductsResult []domain.Product
	listProductsErr    error
	lastListParams     outbound.ProductListParams

	reserveInputs         []catalog.ReserveStockInput
	reserveStockErrOnCall map[int]error
	releaseInputs         []catalog.ReleaseStockInput
}

func (s *stubCatalogService) GetProduct(_ context.Context, _ uuid.UUID) (catalog.GetProductResult, error) {
	return s.getProductResult, s.getProductErr
}

func (s *stubCatalogService) GetProductBySKU(_ context.Context, _ string) (catalog.GetProductResult, error) {
	return s.getBySKUResult, s.getBySKUErr
}

func (s *stubCatalogService) ListProducts(_ context.Context, params outbound.ProductListParams) ([]domain.Product, error) {
	s.lastListParams = params
	return s.listProductsResult, s.listProductsErr
}

func (s *stubCatalogService) ReserveStock(_ context.Context, input catalog.ReserveStockInput) (catalog.ReserveStockResult, error) {
	callIndex := len(s.reserveInputs)
	s.reserveInputs = append(s.reserveInputs, input)
	if err, ok := s.reserveStockErrOnCall[callIndex]; ok {
		return catalog.ReserveStockResult{}, err
	}

	return catalog.ReserveStockResult{}, nil
}

func (s *stubCatalogService) ReleaseStock(_ context.Context, input catalog.ReleaseStockInput) (catalog.ReleaseStockResult, error) {
	s.releaseInputs = append(s.releaseInputs, input)
	return catalog.ReleaseStockResult{Released: true}, nil
}

func TestMapServiceErrorDefault(t *testing.T) {
	err := mapServiceError(errors.New("boom"))
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGetProductBySKU(t *testing.T) {
	productID := uuid.New()

	tests := []struct {
		name         string
		request      *catalogv1.GetProductBySKURequest
		setup        func(*stubCatalogService)
		expectedCode codes.Code
		assert       func(*testing.T, *catalogv1.GetProductBySKUResponse)
	}{
		{
			name:    "success",
			request: &catalogv1.GetProductBySKURequest{Sku: "SKU-1"},
			setup: func(svc *stubCatalogService) {
				svc.getBySKUResult = catalog.GetProductResult{Product: domain.Product{ID: productID, SKU: "SKU-1", Name: "Coffee", Price: 500, Currency: "USD", Status: domain.ProductStatusPublished}}
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *catalogv1.GetProductBySKUResponse) {
				require.Equal(t, "SKU-1", response.GetProduct().GetSku())
				require.Equal(t, productID.String(), response.GetProduct().GetProductId())
			},
		},
		{
			name:         "invalid sku",
			request:      &catalogv1.GetProductBySKURequest{Sku: "   "},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:    "not found",
			request: &catalogv1.GetProductBySKURequest{Sku: "SKU-404"},
			setup: func(svc *stubCatalogService) {
				svc.getBySKUErr = outbound.ErrProductNotFound
			},
			expectedCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubCatalogService{}
			if tt.setup != nil {
				tt.setup(svc)
			}

			server := NewCatalogServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
			response, err := server.GetProductBySKU(context.Background(), tt.request)
			require.Equal(t, tt.expectedCode, status.Code(err))

			if tt.expectedCode != codes.OK {
				require.Nil(t, response)
				return
			}

			require.NotNil(t, response)
			if tt.assert != nil {
				tt.assert(t, response)
			}
		})
	}
}

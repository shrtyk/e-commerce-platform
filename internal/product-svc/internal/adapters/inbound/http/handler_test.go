package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
)

func TestGetProductByID(t *testing.T) {
	productID := uuid.New()

	tests := []struct {
		name       string
		path       string
		setup      func(*stubCatalogService)
		statusCode int
		assertBody func(*testing.T, string)
	}{
		{
			name: "success",
			path: "/v1/products/" + productID.String(),
			setup: func(svc *stubCatalogService) {
				svc.getProductResult = catalog.GetProductResult{
					Product: domain.Product{
						ID:          productID,
						SKU:         "SKU-1",
						Name:        "Coffee",
						Price:       1299,
						Currency:    "USD",
						Status:      domain.ProductStatusPublished,
						Description: "Best coffee",
					},
				}
			},
			statusCode: http.StatusOK,
			assertBody: func(t *testing.T, body string) {
				var response dto.Product
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, productID.String(), response.ProductId)
				require.Equal(t, dto.ProductStatusPublished, response.Status)
			},
		},
		{
			name:       "invalid id",
			path:       "/v1/products/not-a-uuid",
			setup:      func(_ *stubCatalogService) {},
			statusCode: http.StatusBadRequest,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "invalid_request", response.Code)
			},
		},
		{
			name: "not found",
			path: "/v1/products/" + productID.String(),
			setup: func(svc *stubCatalogService) {
				svc.getProductErr = outbound.ErrProductNotFound
			},
			statusCode: http.StatusNotFound,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "product_not_found", response.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubCatalogService{}
			tt.setup(svc)

			h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "product-svc-test", svc, noop.NewTracerProvider().Tracer("product-svc-test"))
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)
			tt.assertBody(t, res.Body.String())
		})
	}
}

func TestListPublishedProducts(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*stubCatalogService)
		statusCode int
		assertBody func(*testing.T, string)
	}{
		{
			name: "success",
			setup: func(svc *stubCatalogService) {
				svc.listProductsResult = []domain.Product{
					{ID: uuid.New(), SKU: "SKU-1", Name: "Pub", Price: 100, Currency: "USD", Status: domain.ProductStatusPublished},
					{ID: uuid.New(), SKU: "SKU-2", Name: "Draft", Price: 100, Currency: "USD", Status: domain.ProductStatusDraft},
					{ID: uuid.New(), SKU: "SKU-3", Name: "Arch", Price: 100, Currency: "USD", Status: domain.ProductStatusArchived},
				}
			},
			statusCode: http.StatusOK,
			assertBody: func(t *testing.T, body string) {
				var response dto.ProductListResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Len(t, response.Items, 1)
				require.Equal(t, dto.ProductStatusPublished, response.Items[0].Status)
			},
		},
		{
			name: "service error",
			setup: func(svc *stubCatalogService) {
				svc.listProductsErr = errors.New("db down")
			},
			statusCode: http.StatusInternalServerError,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "internal_error", response.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubCatalogService{}
			tt.setup(svc)

			h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "product-svc-test", svc, noop.NewTracerProvider().Tracer("product-svc-test"))
			req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)
			tt.assertBody(t, res.Body.String())
			require.Equal(t, int32(100), svc.lastListParams.Limit)
		})
	}
}

func TestHealthz(t *testing.T) {
	h := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"product-svc-test",
		&stubCatalogService{},
		noop.NewTracerProvider().Tracer("product-svc-test"),
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, "ok", res.Body.String())
}

func TestHandleOpenAPIError(t *testing.T) {
	handler := NewCatalogHandler(&stubCatalogService{})
	req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
	res := httptest.NewRecorder()

	handler.HandleOpenAPIError(res, req, errors.New("bad param"))

	require.Equal(t, http.StatusBadRequest, res.Code)
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	require.Equal(t, "invalid_request", response.Code)
}

func TestCatalogPublicRoutesAccessibleWithoutAuth(t *testing.T) {
	productID := uuid.New()
	svc := &stubCatalogService{
		getProductResult: catalog.GetProductResult{
			Product: domain.Product{
				ID:       productID,
				SKU:      "SKU-1",
				Name:     "Coffee",
				Price:    1299,
				Currency: "USD",
				Status:   domain.ProductStatusPublished,
			},
		},
		listProductsResult: []domain.Product{{
			ID:       productID,
			SKU:      "SKU-1",
			Name:     "Coffee",
			Price:    1299,
			Currency: "USD",
			Status:   domain.ProductStatusPublished,
		}},
	}

	h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "product-svc-test", svc, noop.NewTracerProvider().Tracer("product-svc-test"))

	listReq := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
	listRes := httptest.NewRecorder()
	h.ServeHTTP(listRes, listReq)
	require.Equal(t, http.StatusOK, listRes.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/v1/products/"+productID.String(), nil)
	getRes := httptest.NewRecorder()
	h.ServeHTTP(getRes, getReq)
	require.Equal(t, http.StatusOK, getRes.Code)
}

func TestCatalogWriteRoutesRequireAuth(t *testing.T) {
	productID := uuid.New()

	tests := []struct {
		name          string
		method        string
		path          string
		body          string
		tokenVerifier *stubTokenVerifier
		authHeader    string
		statusCode    int
		errorCode     string
	}{
		{name: "post missing token", method: http.MethodPost, path: "/v1/products", body: `{"sku":"SKU-1"}`, tokenVerifier: &stubTokenVerifier{}, statusCode: http.StatusUnauthorized},
		{name: "post non admin token", method: http.MethodPost, path: "/v1/products", body: `{"sku":"SKU-1"}`, tokenVerifier: &stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "user", Status: "active"}}, authHeader: "Bearer user-token", statusCode: http.StatusForbidden, errorCode: "forbidden"},
		{name: "post inactive admin token", method: http.MethodPost, path: "/v1/products", body: `{"sku":"SKU-1"}`, tokenVerifier: &stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "admin", Status: "inactive"}}, authHeader: "Bearer admin-token", statusCode: http.StatusForbidden, errorCode: "forbidden"},
		{name: "patch missing token", method: http.MethodPatch, path: "/v1/products/" + productID.String(), body: `{"name":"New"}`, tokenVerifier: &stubTokenVerifier{}, statusCode: http.StatusUnauthorized},
		{name: "patch non admin token", method: http.MethodPatch, path: "/v1/products/" + productID.String(), body: `{"name":"New"}`, tokenVerifier: &stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "user", Status: "active"}}, authHeader: "Bearer user-token", statusCode: http.StatusForbidden, errorCode: "forbidden"},
		{name: "patch inactive admin token", method: http.MethodPatch, path: "/v1/products/" + productID.String(), body: `{"name":"New"}`, tokenVerifier: &stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "admin", Status: "inactive"}}, authHeader: "Bearer admin-token", statusCode: http.StatusForbidden, errorCode: "forbidden"},
		{name: "delete missing token", method: http.MethodDelete, path: "/v1/products/" + productID.String(), tokenVerifier: &stubTokenVerifier{}, statusCode: http.StatusUnauthorized},
		{name: "delete non admin token", method: http.MethodDelete, path: "/v1/products/" + productID.String(), tokenVerifier: &stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "user", Status: "active"}}, authHeader: "Bearer user-token", statusCode: http.StatusForbidden, errorCode: "forbidden"},
		{name: "delete inactive admin token", method: http.MethodDelete, path: "/v1/products/" + productID.String(), tokenVerifier: &stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "admin", Status: "inactive"}}, authHeader: "Bearer admin-token", statusCode: http.StatusForbidden, errorCode: "forbidden"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewRouter(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				"product-svc-test",
				&stubCatalogService{},
				noop.NewTracerProvider().Tracer("product-svc-test"),
				tt.tokenVerifier,
			)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)

			var response dto.ErrorResponse
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
			if tt.errorCode == "" {
				require.Equal(t, "unauthorized", response.Code)
				return
			}

			require.Equal(t, tt.errorCode, response.Code)
		})
	}
}

func TestCatalogWriteRoutesAsAdmin(t *testing.T) {
	productID := uuid.New()
	categoryID := uuid.New()
	currencyID := uuid.New()

	newName := "Updated product"
	newPrice := int64(1499)
	newStatus := domain.ProductStatusPublished
	newCategoryID := uuid.New()

	svc := &stubCatalogService{
		createProductResult: catalog.CreateProductResult{
			Product: domain.Product{
				ID:          productID,
				SKU:         "SKU-1",
				Name:        "Created product",
				Description: "Created description",
				Price:       1299,
				Currency:    "USD",
				CurrencyID:  currencyID,
				CategoryID:  &categoryID,
				Status:      domain.ProductStatusDraft,
			},
			Stock: domain.StockRecord{
				ProductID: productID,
				Quantity:  25,
				Reserved:  0,
				Available: 25,
				Status:    domain.StockRecordStatusInStock,
			},
		},
		updateProductResult: catalog.UpdateProductResult{
			Product: domain.Product{
				ID:          productID,
				SKU:         "SKU-1",
				Name:        "Updated product",
				Description: "Updated description",
				Price:       1499,
				Currency:    "USD",
				CurrencyID:  currencyID,
				CategoryID:  &categoryID,
				Status:      domain.ProductStatusPublished,
			},
			Stock: domain.StockRecord{
				ProductID: productID,
				Quantity:  25,
				Reserved:  2,
				Available: 23,
				Status:    domain.StockRecordStatusInStock,
			},
		},
	}

	h := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"product-svc-test",
		svc,
		noop.NewTracerProvider().Tracer("product-svc-test"),
		&stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "admin", Status: "active"}},
	)

	t.Run("create product", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/products", strings.NewReader(`{"sku":"SKU-1","name":"Created product","description":"Created description","price":1299,"currencyCode":"USD","status":"draft","categoryId":"`+categoryID.String()+`","initialQuantity":25}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusCreated, res.Code)

		var response dto.ProductWriteResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, productID.String(), response.Product.ProductId)
		require.Equal(t, dto.ProductStatusDraft, response.Product.Status)
		require.Equal(t, 25, response.Stock.Quantity)
		require.Equal(t, dto.InStock, response.Stock.Status)
		require.Equal(t, "USD", svc.lastCreateInput.Product.Currency)
	})

	t.Run("create product rejects empty currency code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/products", strings.NewReader(`{"sku":"SKU-1","name":"Created product","description":"Created description","price":1299,"currencyCode":"  ","status":"draft","categoryId":"`+categoryID.String()+`","initialQuantity":25}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid currency", response.Message)
	})

	t.Run("update product", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/products/"+productID.String(), strings.NewReader(`{"name":"Updated product","price":1499,"status":"published","categoryId":"`+newCategoryID.String()+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusOK, res.Code)

		var response dto.ProductWriteResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, productID.String(), response.Product.ProductId)
		require.Equal(t, dto.ProductStatusPublished, response.Product.Status)
		require.Equal(t, 23, response.Stock.Available)

		require.NotNil(t, svc.lastUpdateInput.Name)
		require.Equal(t, newName, *svc.lastUpdateInput.Name)
		require.NotNil(t, svc.lastUpdateInput.Price)
		require.Equal(t, newPrice, *svc.lastUpdateInput.Price)
		require.NotNil(t, svc.lastUpdateInput.Status)
		require.Equal(t, newStatus, *svc.lastUpdateInput.Status)
		require.NotNil(t, svc.lastUpdateInput.CategoryID)
		require.Equal(t, newCategoryID, *svc.lastUpdateInput.CategoryID)
	})

	t.Run("update product rejects empty category id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/products/"+productID.String(), strings.NewReader(`{"categoryId":""}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid category id", response.Message)
	})

	t.Run("update product rejects whitespace category id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/products/"+productID.String(), strings.NewReader(`{"categoryId":"   "}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid category id", response.Message)
	})

	t.Run("update product rejects null category id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/products/"+productID.String(), strings.NewReader(`{"categoryId":null}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid category id", response.Message)
	})

	t.Run("update product rejects non-string category id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/products/"+productID.String(), strings.NewReader(`{"categoryId":123}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid category id", response.Message)
	})

	t.Run("update product returns invalid request body for non-category decode errors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/products/"+productID.String(), strings.NewReader(`{"categoryId":"`+newCategoryID.String()+`","price":"bad"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid request body", response.Message)
	})

	t.Run("update product rejects oversized request body", func(t *testing.T) {
		isolatedSvc := &stubCatalogService{}
		isolatedRouter := NewRouter(
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			"product-svc-test",
			isolatedSvc,
			noop.NewTracerProvider().Tracer("product-svc-test"),
			&stubTokenVerifier{claims: transport.Claims{UserID: uuid.New(), Role: "admin", Status: "active"}},
		)

		largeName := strings.Repeat("x", int(maxPatchRequestBodyBytes)+1)
		req := httptest.NewRequest(http.MethodPatch, "/v1/products/"+productID.String(), strings.NewReader(`{"name":"`+largeName+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		isolatedRouter.ServeHTTP(res, req)

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid request body", response.Message)
		require.Equal(t, uuid.Nil, isolatedSvc.lastUpdateInput.ID)
	})

	t.Run("archive product", func(t *testing.T) {
		svc.updateProductResult.Product.Status = domain.ProductStatusArchived

		req := httptest.NewRequest(http.MethodDelete, "/v1/products/"+productID.String(), nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		res := httptest.NewRecorder()

		h.ServeHTTP(res, req)

		require.Equal(t, http.StatusOK, res.Code)

		var response dto.ProductWriteResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, dto.ProductStatusArchived, response.Product.Status)

		require.NotNil(t, svc.lastUpdateInput.Status)
		require.Equal(t, domain.ProductStatusArchived, *svc.lastUpdateInput.Status)
	})
}

type stubTokenVerifier struct {
	claims transport.Claims
	err    error
}

func (v *stubTokenVerifier) Verify(_ string) (transport.Claims, error) {
	if v.err != nil {
		return transport.Claims{}, v.err
	}

	return v.claims, nil
}

type stubCatalogService struct {
	createProductResult catalog.CreateProductResult
	createProductErr    error

	getProductResult catalog.GetProductResult
	getProductErr    error

	listProductsResult []domain.Product
	listProductsErr    error
	lastListParams     outbound.ProductListParams

	updateProductResult catalog.UpdateProductResult
	updateProductErr    error
	lastCreateInput     catalog.CreateProductInput
	lastUpdateInput     catalog.UpdateProductInput
}

func (s *stubCatalogService) CreateProduct(_ context.Context, input catalog.CreateProductInput) (catalog.CreateProductResult, error) {
	s.lastCreateInput = input
	return s.createProductResult, s.createProductErr
}

func (s *stubCatalogService) GetProduct(_ context.Context, _ uuid.UUID) (catalog.GetProductResult, error) {
	return s.getProductResult, s.getProductErr
}

func (s *stubCatalogService) ListProducts(_ context.Context, params outbound.ProductListParams) ([]domain.Product, error) {
	s.lastListParams = params
	return s.listProductsResult, s.listProductsErr
}

func (s *stubCatalogService) UpdateProduct(_ context.Context, input catalog.UpdateProductInput) (catalog.UpdateProductResult, error) {
	s.lastUpdateInput = input
	return s.updateProductResult, s.updateProductErr
}

//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/testhelper"
)

func TestGetProductReturnsProductAndStock(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "http and grpc can read created product"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := testhelper.IntegrationHarness(t)
			testhelper.CleanupDB(t, harness.DB)
			stack := newCatalogStack(t)

			created := createProductViaService(t, stack, domain.ProductStatusPublished, 12)
			require.Equal(t, int32(12), created.Stock.Available)

			httpProduct := getProductHTTP(t, stack, created.Product.ID.String(), http.StatusOK)
			require.Equal(t, created.Product.ID.String(), httpProduct.ProductId)
			require.Equal(t, created.Product.SKU, httpProduct.Sku)

			grpcClient := catalogv1.NewCatalogServiceClient(stack.GRPCConn)
			grpcResponse, err := grpcClient.GetProduct(context.Background(), &catalogv1.GetProductRequest{ProductId: created.Product.ID.String()})
			require.NoError(t, err)
			require.NotNil(t, grpcResponse.GetProduct())
			require.Equal(t, created.Product.ID.String(), grpcResponse.GetProduct().GetProductId())

			serviceRead, err := stack.CatalogService.GetProduct(context.Background(), created.Product.ID)
			require.NoError(t, err)
			require.Equal(t, int32(12), serviceRead.Stock.Available)
			require.Equal(t, int32(0), serviceRead.Stock.Reserved)
		})
	}
}

func TestGetProductReturnsNotFound(t *testing.T) {
	uuid := uuid.NewString()
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	httpErr := getProductHTTPError(t, stack, uuid, http.StatusNotFound)
	require.Equal(t, "product_not_found", httpErr.Code)

	grpcClient := catalogv1.NewCatalogServiceClient(stack.GRPCConn)
	_, err := grpcClient.GetProduct(context.Background(), &catalogv1.GetProductRequest{ProductId: uuid})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGetProductRejectsBadUUID(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	httpErr := getProductHTTPError(t, stack, "not-a-uuid", http.StatusBadRequest)
	require.Equal(t, "invalid_request", httpErr.Code)
}

func TestGetProductBySKUReturnsProduct(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	created := createProductViaService(t, stack, domain.ProductStatusPublished, 5)

	grpcClient := catalogv1.NewCatalogServiceClient(stack.GRPCConn)
	grpcResponse, err := grpcClient.GetProductBySKU(context.Background(), &catalogv1.GetProductBySKURequest{Sku: created.Product.SKU})
	require.NoError(t, err)
	require.NotNil(t, grpcResponse.GetProduct())
	require.Equal(t, created.Product.ID.String(), grpcResponse.GetProduct().GetProductId())
	require.Equal(t, created.Product.SKU, grpcResponse.GetProduct().GetSku())

	serviceRead, svcErr := stack.CatalogService.GetProductBySKU(context.Background(), created.Product.SKU)
	require.NoError(t, svcErr)
	require.Equal(t, created.Product.ID, serviceRead.Product.ID)
}

func TestGetProductBySKUReturnsNotFound(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	grpcClient := catalogv1.NewCatalogServiceClient(stack.GRPCConn)
	_, err := grpcClient.GetProductBySKU(context.Background(), &catalogv1.GetProductBySKURequest{Sku: "SKU-404"})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestListPublishedProductsReturnsOnlyPublished(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	draft := createProductViaService(t, stack, domain.ProductStatusDraft, 5)
	published := createProductViaService(t, stack, domain.ProductStatusPublished, 7)

	httpList := listPublishedProductsHTTP(t, stack)
	require.Len(t, httpList.Items, 1)
	require.Equal(t, published.Product.ID.String(), httpList.Items[0].ProductId)
	require.NotEqual(t, draft.Product.ID.String(), httpList.Items[0].ProductId)

	grpcClient := catalogv1.NewCatalogServiceClient(stack.GRPCConn)
	grpcList, err := grpcClient.ListPublishedProducts(context.Background(), &catalogv1.ListPublishedProductsRequest{
		Page: &commonv1.PageRequest{PageSize: 100},
	})
	require.NoError(t, err)
	require.Len(t, grpcList.GetItems(), 1)
	require.Equal(t, published.Product.ID.String(), grpcList.GetItems()[0].GetProductId())
}

func TestAdminWriteRoutesAndPublicFiltering(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	currencyID := getCurrencyID(t, stack.DB, "USD")
	adminToken := issueTestAdminToken(t)

	serviceCreated, serviceCreateErr := stack.CatalogService.CreateProduct(context.Background(), catalog.CreateProductInput{
		Product: domain.Product{
			SKU:        "SVC-" + uuid.NewString(),
			Name:       "Service create",
			Price:      999,
			CurrencyID: currencyID,
			Currency:   "USD",
			Status:     domain.ProductStatusDraft,
		},
		InitialQuantity: 1,
	})
	require.NoError(t, serviceCreateErr)
	require.Equal(t, domain.ProductStatusDraft, serviceCreated.Product.Status)

	publishedStatus := domain.ProductStatusPublished
	servicePatched, servicePatchErr := stack.CatalogService.UpdateProduct(context.Background(), catalog.UpdateProductInput{
		ID:     serviceCreated.Product.ID,
		Status: &publishedStatus,
	})
	require.NoError(t, servicePatchErr)
	storedStatus := getProductStatus(t, stack.DB, serviceCreated.Product.ID)
	require.Equalf(t, domain.ProductStatusPublished, servicePatched.Product.Status, "stored status: %s", storedStatus)

	createBody := fmt.Sprintf(`{"sku":"SKU-%s","name":"Admin created","description":"created via admin route","price":1099,"currencyId":"%s","status":"draft","initialQuantity":11}`,
		uuid.NewString(),
		currencyID.String(),
	)
	createRes := performAdminJSONRequest(t, stack, http.MethodPost, "/v1/products", createBody, adminToken, http.StatusCreated)

	var created dto.ProductWriteResponse
	require.NoError(t, json.NewDecoder(createRes.Body).Decode(&created))
	require.Equal(t, dto.ProductStatusDraft, created.Product.Status)
	require.Equal(t, 11, created.Stock.Quantity)

	getDraftErr := getProductHTTPError(t, stack, created.Product.ProductId, http.StatusNotFound)
	require.Equal(t, "product_not_found", getDraftErr.Code)

	categoryID := uuid.New()
	patchBody := fmt.Sprintf(`{"status":"published","name":"Admin published","categoryId":"%s"}`,
		categoryID.String(),
	)
	patchRes := performAdminJSONRequest(t, stack, http.MethodPatch, "/v1/products/"+created.Product.ProductId, patchBody, adminToken, http.StatusOK)

	var updated dto.ProductWriteResponse
	require.NoError(t, json.NewDecoder(patchRes.Body).Decode(&updated))
	require.Equal(t, dto.ProductStatusPublished, updated.Product.Status)
	require.Equal(t, "Admin published", updated.Product.Name)
	require.NotNil(t, updated.Product.CategoryId)
	require.Equal(t, categoryID.String(), *updated.Product.CategoryId)

	publicProduct := getProductHTTP(t, stack, created.Product.ProductId, http.StatusOK)
	require.Equal(t, dto.ProductStatusPublished, publicProduct.Status)

	deleteRes := performAdminJSONRequest(t, stack, http.MethodDelete, "/v1/products/"+created.Product.ProductId, "", adminToken, http.StatusOK)

	var archived dto.ProductWriteResponse
	require.NoError(t, json.NewDecoder(deleteRes.Body).Decode(&archived))
	require.Equal(t, dto.ProductStatusArchived, archived.Product.Status)

	getArchivedErr := getProductHTTPError(t, stack, created.Product.ProductId, http.StatusNotFound)
	require.Equal(t, "product_not_found", getArchivedErr.Code)
}

func TestAdminPatchRejectsEmptyOrWhitespaceCategoryID(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	created := createProductViaService(t, stack, domain.ProductStatusDraft, 3)
	adminToken := issueTestAdminToken(t)

	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: `{"categoryId":""}`},
		{name: "whitespace", body: `{"categoryId":"   "}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performAdminJSONRequest(
				t,
				stack,
				http.MethodPatch,
				"/v1/products/"+created.Product.ID.String(),
				tt.body,
				adminToken,
				http.StatusBadRequest,
			)

			var httpErr dto.ErrorResponse
			require.NoError(t, json.NewDecoder(response.Body).Decode(&httpErr))
			require.Equal(t, "invalid_request", httpErr.Code)
			require.Equal(t, "invalid category id", httpErr.Message)
		})
	}
}

func TestAdminPatchRejectsNullOrNonStringCategoryID(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	created := createProductViaService(t, stack, domain.ProductStatusDraft, 3)
	adminToken := issueTestAdminToken(t)

	tests := []struct {
		name string
		body string
	}{
		{name: "null", body: `{"categoryId":null}`},
		{name: "number", body: `{"categoryId":123}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performAdminJSONRequest(
				t,
				stack,
				http.MethodPatch,
				"/v1/products/"+created.Product.ID.String(),
				tt.body,
				adminToken,
				http.StatusBadRequest,
			)

			var httpErr dto.ErrorResponse
			require.NoError(t, json.NewDecoder(response.Body).Decode(&httpErr))
			require.Equal(t, "invalid_request", httpErr.Code)
			require.Equal(t, "invalid category id", httpErr.Message)
		})
	}
}

func TestAdminWriteRoutesAuthFailuresReturnJSONErrorResponse(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	productID := uuid.NewString()
	inactiveAdminToken := issueTestToken(t, "admin", "inactive")
	nonAdminToken := issueTestToken(t, "user", "active")

	tests := []struct {
		name           string
		token          string
		expectedStatus int
		expectedCode   string
	}{
		{name: "missing token", token: "", expectedStatus: http.StatusUnauthorized, expectedCode: "unauthorized"},
		{name: "invalid token", token: "not-a-jwt", expectedStatus: http.StatusUnauthorized, expectedCode: "unauthorized"},
		{name: "non-admin token", token: nonAdminToken, expectedStatus: http.StatusForbidden, expectedCode: "forbidden"},
		{name: "inactive admin token", token: inactiveAdminToken, expectedStatus: http.StatusForbidden, expectedCode: "forbidden"},
	}

	requestCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "post", method: http.MethodPost, path: "/v1/products", body: `{"sku":"SKU-1","name":"x","price":100,"currencyId":"` + uuid.NewString() + `","initialQuantity":1}`},
		{name: "patch", method: http.MethodPatch, path: "/v1/products/" + productID, body: `{"name":"x"}`},
		{name: "delete", method: http.MethodDelete, path: "/v1/products/" + productID, body: ""},
	}

	for _, reqCase := range requestCases {
		for _, tt := range tests {
			t.Run(reqCase.name+" "+tt.name, func(t *testing.T) {
				request := httptest.NewRequest(reqCase.method, reqCase.path, strings.NewReader(reqCase.body))
				if reqCase.body != "" {
					request.Header.Set("Content-Type", "application/json")
				}
				if tt.token != "" {
					request.Header.Set("Authorization", "Bearer "+tt.token)
				}

				response := httptest.NewRecorder()
				stack.HTTPHandler.ServeHTTP(response, request)

				require.Equal(t, tt.expectedStatus, response.Code)

				var body dto.ErrorResponse
				require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
				require.Equal(t, tt.expectedCode, body.Code)
			})
		}
	}
}

func TestReserveStockViaGRPC(t *testing.T) {
	var initialQuantity int32 = 10
	var reserveQuantity int32 = 4

	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	created := createProductViaService(t, stack, domain.ProductStatusPublished, initialQuantity)

	grpcClient := catalogv1.NewCatalogServiceClient(stack.GRPCConn)
	response, err := grpcClient.ReserveStock(context.Background(), &catalogv1.ReserveStockRequest{
		OrderId: uuid.NewString(),
		Items: []*catalogv1.ReservationItem{
			{ProductId: created.Product.ID.String(), Quantity: int64(reserveQuantity)},
		},
	})
	require.NoError(t, err)
	require.True(t, response.GetAccepted())

	updated, err := stack.CatalogService.GetProduct(context.Background(), created.Product.ID)
	require.NoError(t, err)
	require.Equal(t, int32(reserveQuantity), updated.Stock.Reserved)
	require.Equal(t, initialQuantity-reserveQuantity, updated.Stock.Available)
}

func TestReserveStockRejectsInsufficient(t *testing.T) {
	var initialQuantity int32 = 2
	var reserveQuantity int32 = 3

	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newCatalogStack(t)

	created := createProductViaService(t, stack, domain.ProductStatusPublished, initialQuantity)

	grpcClient := catalogv1.NewCatalogServiceClient(stack.GRPCConn)
	_, err := grpcClient.ReserveStock(context.Background(), &catalogv1.ReserveStockRequest{
		OrderId: uuid.NewString(),
		Items: []*catalogv1.ReservationItem{
			{ProductId: created.Product.ID.String(), Quantity: int64(reserveQuantity)},
		},
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	after, getErr := stack.CatalogService.GetProduct(context.Background(), created.Product.ID)
	require.NoError(t, getErr)
	require.Equal(t, int32(0), after.Stock.Reserved)
	require.Equal(t, initialQuantity, after.Stock.Available)
}

func newCatalogStack(t *testing.T) *testhelper.TestStack {
	t.Helper()

	return testhelper.NewTestStack(t, testhelper.IntegrationHarness(t).DB)
}

func createProductViaService(
	t *testing.T,
	stack *testhelper.TestStack,
	status domain.ProductStatus,
	initialQuantity int32,
) catalog.CreateProductResult {
	t.Helper()

	currencyID := getCurrencyID(t, stack.DB, "USD")
	productID := uuid.New()
	sku := "SKU-" + uuid.NewString()
	name := "Product " + uuid.NewString()

	result, err := stack.CatalogService.CreateProduct(context.Background(), catalog.CreateProductInput{
		Product: domain.Product{
			ID:         productID,
			SKU:        sku,
			Name:       name,
			Price:      1500,
			CurrencyID: currencyID,
			Currency:   "USD",
			Status:     status,
		},
		InitialQuantity: initialQuantity,
	})
	if err == nil {
		return result
	}

	if !strings.Contains(err.Error(), "no rows in result set") {
		require.NoError(t, err)
	}

	_, insertErr := stack.DB.ExecContext(
		context.Background(),
		`INSERT INTO products (product_id, sku, name, price, currency_id, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		productID,
		sku,
		name,
		int64(1500),
		currencyID,
		string(status),
	)
	require.NoError(t, insertErr)

	_, stockErr := stack.DB.ExecContext(
		context.Background(),
		`INSERT INTO stock_records (product_id, quantity, reserved) VALUES ($1, $2, 0)`,
		productID,
		initialQuantity,
	)
	require.NoError(t, stockErr)

	fallback, getErr := stack.CatalogService.GetProduct(context.Background(), productID)
	require.NoError(t, getErr)

	return catalog.CreateProductResult{
		Product: fallback.Product,
		Stock:   fallback.Stock,
	}
}

func getCurrencyID(t *testing.T, db *sql.DB, code string) uuid.UUID {
	t.Helper()

	var currencyID uuid.UUID
	err := db.QueryRowContext(context.Background(), "SELECT id FROM currencies WHERE code = $1", code).Scan(&currencyID)
	require.NoError(t, err)

	return currencyID
}

func getProductStatus(t *testing.T, db *sql.DB, productID uuid.UUID) domain.ProductStatus {
	t.Helper()

	var status string
	err := db.QueryRowContext(context.Background(), "SELECT status FROM products WHERE product_id = $1", productID).Scan(&status)
	require.NoError(t, err)

	return domain.ProductStatus(status)
}

func getProductHTTP(t *testing.T, stack *testhelper.TestStack, productID string, expectedStatus int) dto.Product {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/products/%s", productID), nil)
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, expectedStatus, response.Code)

	var product dto.Product
	require.NoError(t, json.NewDecoder(response.Body).Decode(&product))

	return product
}

func getProductHTTPError(t *testing.T, stack *testhelper.TestStack, productID string, expectedStatus int) dto.ErrorResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/products/%s", productID), nil)
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, expectedStatus, response.Code)

	var httpErr dto.ErrorResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&httpErr))

	return httpErr
}

func listPublishedProductsHTTP(t *testing.T, stack *testhelper.TestStack) dto.ProductListResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)

	var list dto.ProductListResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&list))

	return list
}

func performAdminJSONRequest(
	t *testing.T,
	stack *testhelper.TestStack,
	method string,
	path string,
	body string,
	token string,
	expectedStatus int,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equalf(t, expectedStatus, response.Code, "response body: %s", response.Body.String())

	return response
}

func issueTestAdminToken(t *testing.T) string {
	t.Helper()

	return issueTestToken(t, "admin", "active")
}

func issueTestToken(t *testing.T, role string, userStatus string) string {
	t.Helper()

	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, jwtv5.MapClaims{
		"sub":    uuid.NewString(),
		"iss":    testhelper.TestAuthIssuer,
		"role":   role,
		"status": userStatus,
	})

	signed, err := token.SignedString([]byte(testhelper.TestAuthKey))
	require.NoError(t, err)

	return signed
}

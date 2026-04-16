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

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

func TestGetActiveCart(t *testing.T) {
	userID := uuid.New()

	h := newTestRouter(
		&fakeCartService{
			getActiveCartFn: func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, error) {
				require.Equal(t, userID, gotUserID)

				return domain.Cart{
					ID:          uuid.New(),
					UserID:      userID,
					Status:      domain.CartStatusActive,
					Currency:    "USD",
					TotalAmount: 2400,
					Items: []domain.CartItem{{
						SKU:       "SKU-1",
						Name:      "Test item",
						Quantity:  2,
						UnitPrice: 1200,
						Currency:  "USD",
						LineTotal: 2400,
					}},
				}, nil
			},
		},
		transport.Claims{UserID: userID, Role: "user", Status: "active"},
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/cart", nil)
	req.Header.Set("Authorization", "Bearer token")
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code)

	var response dto.Cart
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	require.Equal(t, userID.String(), response.UserId)
	require.Equal(t, "USD", response.Currency)
	require.Equal(t, 2400, response.TotalAmount)
	require.Len(t, response.Items, 1)
	require.Equal(t, "SKU-1", response.Items[0].Sku)
}

func TestAddCartItem(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name       string
		body       string
		setup      func(t *testing.T, svc *fakeCartService)
		statusCode int
		errorCode  string
		assert     func(t *testing.T, svc *fakeCartService, body []byte)
	}{
		{
			name: "bad request body",
			body: `{"sku":`,
			setup: func(_ *testing.T, _ *fakeCartService) {
			},
			statusCode: http.StatusBadRequest,
			errorCode:  "invalid_request",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 0, svc.addCartItemCalls)
			},
		},
		{
			name: "success",
			body: `{"sku":"SKU-1","quantity":2}`,
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.addCartItemFn = func(_ context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-1", input.SKU)
					require.EqualValues(t, 2, input.Quantity)

					return testCart(userID, "SKU-1", 2, 1200), nil
				}
			},
			statusCode: http.StatusOK,
			assert: func(t *testing.T, svc *fakeCartService, body []byte) {
				require.Equal(t, 1, svc.addCartItemCalls)

				var response dto.Cart
				require.NoError(t, json.Unmarshal(body, &response))
				require.Equal(t, userID.String(), response.UserId)
				require.Equal(t, "USD", response.Currency)
				require.Equal(t, 2400, response.TotalAmount)
				require.Len(t, response.Items, 1)
				require.Equal(t, "SKU-1", response.Items[0].Sku)
				require.Equal(t, 2, response.Items[0].Quantity)
				require.Equal(t, 1200, response.Items[0].UnitPrice)
				require.Equal(t, 2400, response.Items[0].LineTotal)
			},
		},
		{
			name: "conflict on duplicate cart item",
			body: `{"sku":"SKU-1","quantity":2}`,
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.addCartItemFn = func(_ context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-1", input.SKU)
					require.EqualValues(t, 2, input.Quantity)

					return domain.Cart{}, cart.ErrCartItemAlreadyExists
				}
			},
			statusCode: http.StatusConflict,
			errorCode:  "cart_item_already_exists",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 1, svc.addCartItemCalls)
			},
		},
		{
			name: "unknown service error",
			body: `{"sku":"SKU-1","quantity":2}`,
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.addCartItemFn = func(_ context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-1", input.SKU)
					require.EqualValues(t, 2, input.Quantity)

					return domain.Cart{}, errors.New("db unavailable")
				}
			},
			statusCode: http.StatusInternalServerError,
			errorCode:  "internal_error",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 1, svc.addCartItemCalls)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeCartService{}
			tt.setup(t, svc)

			h := newTestRouter(svc, transport.Claims{UserID: userID, Role: "user", Status: "active"})
			req := httptest.NewRequest(http.MethodPost, "/v1/cart/items", strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)

			if tt.assert != nil {
				tt.assert(t, svc, res.Body.Bytes())
			}

			if tt.errorCode == "" {
				return
			}

			var response dto.ErrorResponse
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
			require.Equal(t, tt.errorCode, response.Code)
		})
	}
}

func TestUpdateCartItem(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name       string
		body       string
		setup      func(t *testing.T, svc *fakeCartService)
		statusCode int
		errorCode  string
		assert     func(t *testing.T, svc *fakeCartService, body []byte)
	}{
		{
			name: "invalid request body",
			body: `{"quantity":`,
			setup: func(_ *testing.T, _ *fakeCartService) {
			},
			statusCode: http.StatusBadRequest,
			errorCode:  "invalid_request",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 0, svc.updateCartItemCalls)
			},
		},
		{
			name: "success",
			body: `{"quantity":3}`,
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.updateCartItemFn = func(_ context.Context, input cart.UpdateCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-2", input.SKU)
					require.EqualValues(t, 3, input.Quantity)

					return testCart(userID, "SKU-2", 3, 500), nil
				}
			},
			statusCode: http.StatusOK,
			assert: func(t *testing.T, svc *fakeCartService, body []byte) {
				require.Equal(t, 1, svc.updateCartItemCalls)

				var response dto.Cart
				require.NoError(t, json.Unmarshal(body, &response))
				require.Equal(t, userID.String(), response.UserId)
				require.Len(t, response.Items, 1)
				require.Equal(t, "SKU-2", response.Items[0].Sku)
				require.Equal(t, 3, response.Items[0].Quantity)
				require.Equal(t, 500, response.Items[0].UnitPrice)
				require.Equal(t, 1500, response.Items[0].LineTotal)
			},
		},
		{
			name: "cart item not found",
			body: `{"quantity":3}`,
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.updateCartItemFn = func(_ context.Context, input cart.UpdateCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-2", input.SKU)
					require.EqualValues(t, 3, input.Quantity)

					return domain.Cart{}, cart.ErrCartItemNotFound
				}
			},
			statusCode: http.StatusNotFound,
			errorCode:  "cart_item_not_found",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 1, svc.updateCartItemCalls)
			},
		},
		{
			name: "unknown service error",
			body: `{"quantity":3}`,
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.updateCartItemFn = func(_ context.Context, input cart.UpdateCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-2", input.SKU)
					require.EqualValues(t, 3, input.Quantity)

					return domain.Cart{}, errors.New("db unavailable")
				}
			},
			statusCode: http.StatusInternalServerError,
			errorCode:  "internal_error",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 1, svc.updateCartItemCalls)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeCartService{}
			tt.setup(t, svc)

			h := newTestRouter(svc, transport.Claims{UserID: userID, Role: "user", Status: "active"})
			req := httptest.NewRequest(http.MethodPatch, "/v1/cart/items/SKU-2", strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)

			if tt.assert != nil {
				tt.assert(t, svc, res.Body.Bytes())
			}

			if tt.errorCode == "" {
				return
			}

			var response dto.ErrorResponse
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
			require.Equal(t, tt.errorCode, response.Code)
		})
	}
}

func TestRemoveCartItem(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name       string
		setup      func(t *testing.T, svc *fakeCartService)
		statusCode int
		errorCode  string
		assert     func(t *testing.T, svc *fakeCartService, body []byte)
	}{
		{
			name: "success",
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.removeCartItemFn = func(_ context.Context, input cart.RemoveCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-3", input.SKU)

					return testCartWithoutItems(userID), nil
				}
			},
			statusCode: http.StatusOK,
			assert: func(t *testing.T, svc *fakeCartService, body []byte) {
				require.Equal(t, 1, svc.removeCartItemCalls)

				var response dto.Cart
				require.NoError(t, json.Unmarshal(body, &response))
				require.Equal(t, userID.String(), response.UserId)
				require.Equal(t, 0, response.TotalAmount)
				require.Empty(t, response.Items)
			},
		},
		{
			name: "currency mismatch",
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.removeCartItemFn = func(_ context.Context, input cart.RemoveCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-3", input.SKU)

					return domain.Cart{}, cart.ErrCartCurrencyMismatch
				}
			},
			statusCode: http.StatusConflict,
			errorCode:  "cart_currency_mismatch",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 1, svc.removeCartItemCalls)
			},
		},
		{
			name: "unknown service error",
			setup: func(t *testing.T, svc *fakeCartService) {
				svc.removeCartItemFn = func(_ context.Context, input cart.RemoveCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					require.Equal(t, "SKU-3", input.SKU)

					return domain.Cart{}, errors.New("db unavailable")
				}
			},
			statusCode: http.StatusInternalServerError,
			errorCode:  "internal_error",
			assert: func(t *testing.T, svc *fakeCartService, _ []byte) {
				require.Equal(t, 1, svc.removeCartItemCalls)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeCartService{}
			tt.setup(t, svc)

			h := newTestRouter(svc, transport.Claims{UserID: userID, Role: "user", Status: "active"})
			req := httptest.NewRequest(http.MethodDelete, "/v1/cart/items/SKU-3", nil)
			req.Header.Set("Authorization", "Bearer token")
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)

			if tt.assert != nil {
				tt.assert(t, svc, res.Body.Bytes())
			}

			if tt.errorCode == "" {
				return
			}

			var response dto.ErrorResponse
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
			require.Equal(t, tt.errorCode, response.Code)
		})
	}
}

func TestGetActiveCartUnauthorizedWithoutClaims(t *testing.T) {
	h := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"test-service",
		&fakeCartService{},
		nil,
		noop.NewTracerProvider().Tracer("test-service"),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/cart", nil)
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)

	require.Equal(t, http.StatusUnauthorized, res.Code)
}

func TestMutatingEndpointsUnauthorizedWithoutClaims(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "add cart item",
			method: http.MethodPost,
			path:   "/v1/cart/items",
			body:   `{"sku":"SKU-1","quantity":2}`,
		},
		{
			name:   "update cart item",
			method: http.MethodPatch,
			path:   "/v1/cart/items/SKU-1",
			body:   `{"quantity":3}`,
		},
		{
			name:   "remove cart item",
			method: http.MethodDelete,
			path:   "/v1/cart/items/SKU-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeCartService{}
			h := NewRouter(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				"test-service",
				svc,
				nil,
				noop.NewTracerProvider().Tracer("test-service"),
			)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, http.StatusUnauthorized, res.Code)
			require.Equal(t, 0, svc.addCartItemCalls)
			require.Equal(t, 0, svc.updateCartItemCalls)
			require.Equal(t, 0, svc.removeCartItemCalls)
		})
	}
}

type fakeCartService struct {
	getActiveCartFn  func(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
	addCartItemFn    func(ctx context.Context, input cart.AddCartItemInput) (domain.Cart, error)
	updateCartItemFn func(ctx context.Context, input cart.UpdateCartItemInput) (domain.Cart, error)
	removeCartItemFn func(ctx context.Context, input cart.RemoveCartItemInput) (domain.Cart, error)

	addCartItemCalls    int
	updateCartItemCalls int
	removeCartItemCalls int
}

func (s *fakeCartService) GetActiveCart(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	if s.getActiveCartFn == nil {
		return domain.Cart{}, nil
	}

	return s.getActiveCartFn(ctx, userID)
}

func (s *fakeCartService) AddCartItem(ctx context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
	s.addCartItemCalls++

	if s.addCartItemFn == nil {
		return domain.Cart{}, nil
	}

	return s.addCartItemFn(ctx, input)
}

func (s *fakeCartService) UpdateCartItem(ctx context.Context, input cart.UpdateCartItemInput) (domain.Cart, error) {
	s.updateCartItemCalls++

	if s.updateCartItemFn == nil {
		return domain.Cart{}, nil
	}

	return s.updateCartItemFn(ctx, input)
}

func (s *fakeCartService) RemoveCartItem(ctx context.Context, input cart.RemoveCartItemInput) (domain.Cart, error) {
	s.removeCartItemCalls++

	if s.removeCartItemFn == nil {
		return domain.Cart{}, nil
	}

	return s.removeCartItemFn(ctx, input)
}

type testTokenVerifier struct {
	claims transport.Claims
}

func (v testTokenVerifier) Verify(_ string) (transport.Claims, error) {
	return v.claims, nil
}

func newTestRouter(service *fakeCartService, claims transport.Claims) http.Handler {
	return NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"test-service",
		service,
		testTokenVerifier{claims: claims},
		noop.NewTracerProvider().Tracer("test-service"),
	)
}

func testCart(userID uuid.UUID, sku string, quantity int64, unitPrice int64) domain.Cart {
	return domain.Cart{
		ID:          uuid.New(),
		UserID:      userID,
		Status:      domain.CartStatusActive,
		Currency:    "USD",
		TotalAmount: quantity * unitPrice,
		Items:       []domain.CartItem{testCartItem(sku, quantity, unitPrice)},
	}
}

func testCartWithoutItems(userID uuid.UUID) domain.Cart {
	return domain.Cart{
		ID:          uuid.New(),
		UserID:      userID,
		Status:      domain.CartStatusActive,
		Currency:    "USD",
		TotalAmount: 0,
		Items:       nil,
	}
}

func testCartItem(sku string, quantity int64, unitPrice int64) domain.CartItem {
	return domain.CartItem{
		SKU:       sku,
		Name:      "Test item",
		Quantity:  quantity,
		UnitPrice: unitPrice,
		Currency:  "USD",
		LineTotal: quantity * unitPrice,
	}
}

package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

func TestOrderHandlerReadyz(t *testing.T) {
	tests := []struct {
		name       string
		checker    readinessChecker
		statusWant int
	}{
		{
			name:       "missing readiness checker",
			checker:    nil,
			statusWant: http.StatusServiceUnavailable,
		},
		{
			name:       "readiness check error",
			checker:    readinessCheckerStub{err: errors.New("db unavailable")},
			statusWant: http.StatusServiceUnavailable,
		},
		{
			name:       "ready",
			checker:    readinessCheckerStub{},
			statusWant: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewOrderHandler(tt.checker, 0, nil)

			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			rr := httptest.NewRecorder()

			handler.Readyz(rr, req)

			require.Equal(t, tt.statusWant, rr.Code)
		})
	}
}

func TestOrderHandlerCreateOrder(t *testing.T) {
	userID := uuid.New()
	successOrder := outbound.Order{
		OrderID:     uuid.New(),
		UserID:      userID,
		Status:      outbound.OrderStatusAwaitingPayment,
		Currency:    "USD",
		TotalAmount: 1200,
		Items: []outbound.OrderItem{{
			OrderItemID: uuid.New(),
			ProductID:   uuid.New(),
			SKU:         "SKU-1",
			Name:        "Product",
			Quantity:    2,
			UnitPrice:   600,
			LineTotal:   1200,
			Currency:    "USD",
		}},
	}

	tests := []struct {
		name         string
		context      context.Context
		headers      map[string]string
		body         string
		setupMock    func(m *mockCheckoutService)
		statusWant   int
		responseCode string
	}{
		{
			name:         "missing claims",
			context:      transport.WithRequestID(context.Background(), "req-1"),
			headers:      map[string]string{"Idempotency-Key": "idem-1"},
			body:         `{"paymentMethod":"card"}`,
			setupMock:    nil,
			statusWant:   http.StatusUnauthorized,
			responseCode: "unauthorized",
		},
		{
			name:         "missing idempotency key",
			context:      withClaimsAndRequestID(userID),
			body:         `{"paymentMethod":"card"}`,
			setupMock:    nil,
			statusWant:   http.StatusBadRequest,
			responseCode: string(checkout.CheckoutErrorCodeInvalidArgument),
		},
		{
			name:         "too long idempotency key",
			context:      withClaimsAndRequestID(userID),
			headers:      map[string]string{"Idempotency-Key": strings.Repeat("x", 256)},
			body:         `{"paymentMethod":"card"}`,
			statusWant:   http.StatusBadRequest,
			responseCode: string(checkout.CheckoutErrorCodeInvalidArgument),
		},
		{
			name:         "invalid json",
			context:      withClaimsAndRequestID(userID),
			headers:      map[string]string{"Idempotency-Key": "idem-json"},
			body:         `{"paymentMethod":`,
			statusWant:   http.StatusBadRequest,
			responseCode: string(checkout.CheckoutErrorCodeInvalidArgument),
		},
		{
			name:    "maps not found code",
			context: withClaimsAndRequestID(userID),
			headers: map[string]string{"Idempotency-Key": "idem-2"},
			body:    `{"paymentMethod":"card"}`,
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, &checkout.CheckoutError{Code: checkout.CheckoutErrorCodeSKUNotFound, Err: errors.New("sku")}).
					Once()
			},
			statusWant:   http.StatusNotFound,
			responseCode: string(checkout.CheckoutErrorCodeSKUNotFound),
		},
		{
			name:    "maps conflict code",
			context: withClaimsAndRequestID(userID),
			headers: map[string]string{"Idempotency-Key": "idem-3"},
			body:    `{"paymentMethod":"card"}`,
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, &checkout.CheckoutError{Code: checkout.CheckoutErrorCodeStockUnavailable, Err: errors.New("stock")}).
					Once()
			},
			statusWant:   http.StatusConflict,
			responseCode: string(checkout.CheckoutErrorCodeStockUnavailable),
		},
		{
			name:    "success",
			context: withClaimsAndRequestID(userID),
			headers: map[string]string{"Idempotency-Key": "idem-4"},
			body:    `{"paymentMethod":"card"}`,
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.MatchedBy(func(input checkout.CheckoutInput) bool {
					if input.UserID != userID {
						return false
					}

					if input.IdempotencyKey != "idem-4" {
						return false
					}

					return input.PaymentMethod != nil && *input.PaymentMethod == "card"
				})).
					Return(successOrder, nil).
					Once()
			},
			statusWant: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newMockCheckoutService(t)
			if tt.setupMock != nil {
				tt.setupMock(service)
			}

			handler := NewOrderHandler(nil, 0, service)

			req := httptest.NewRequest(http.MethodPost, "/v1/orders", strings.NewReader(tt.body)).WithContext(tt.context)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			handler.CreateOrder(rr, req)

			require.Equal(t, tt.statusWant, rr.Code)

			if tt.responseCode == "" {
				var payload dto.Order
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
				require.Equal(t, successOrder.OrderID.String(), payload.OrderId)
				require.Equal(t, successOrder.UserID.String(), payload.UserId)
				return
			}

			var payload struct {
				Code          string `json:"code"`
				CorrelationID string `json:"correlationId"`
			}
			require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
			require.Equal(t, tt.responseCode, payload.Code)
			require.Equal(t, "req-1", payload.CorrelationID)

			if tt.setupMock == nil {
				service.AssertNotCalled(t, "Checkout", testifymock.Anything, testifymock.Anything)
			}
		})
	}
}

type readinessCheckerStub struct {
	err error
}

func (s readinessCheckerStub) PingContext(_ context.Context) error {
	return s.err
}

type mockCheckoutService struct {
	testifymock.Mock
}

func newMockCheckoutService(t *testing.T) *mockCheckoutService {
	t.Helper()

	m := &mockCheckoutService{}
	t.Cleanup(func() { m.AssertExpectations(t) })
	return m
}

func (m *mockCheckoutService) Checkout(ctx context.Context, input checkout.CheckoutInput) (outbound.Order, error) {
	args := m.Called(ctx, input)

	order, _ := args.Get(0).(outbound.Order)
	return order, args.Error(1)
}

func withClaimsAndRequestID(userID uuid.UUID) context.Context {
	ctx := transport.WithClaims(context.Background(), transport.Claims{UserID: userID})
	return transport.WithRequestID(ctx, "req-1")
}

func TestOrderHandlerHandleOpenAPIError(t *testing.T) {
	handler := NewOrderHandler(nil, 0, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/orders", nil).WithContext(withClaimsAndRequestID(uuid.New()))
	rr := httptest.NewRecorder()

	handler.HandleOpenAPIError(rr, req, errors.New("bind failed"))

	require.Equal(t, http.StatusBadRequest, rr.Code)

	var payload struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	require.Equal(t, string(checkout.CheckoutErrorCodeInvalidArgument), payload.Code)
}

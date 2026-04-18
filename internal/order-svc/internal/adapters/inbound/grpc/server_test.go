package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/google/uuid"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

func TestNewServerRequiresTokenVerifier(t *testing.T) {
	require.PanicsWithValue(t, "grpc token verifier is required", func() {
		_ = NewServer(slog.Default(), "order-svc", nil, nil, noop.NewTracerProvider().Tracer("test"))
	})
}

func TestNewServerWithTokenVerifier(t *testing.T) {
	server := NewServer(
		slog.Default(),
		"order-svc",
		nil,
		tokenVerifierStub{},
		noop.NewTracerProvider().Tracer("test"),
	)

	require.NotNil(t, server)
	server.Stop()
}

func TestServerAuthInterceptorProtectsAllOrderRPCsWithoutAuthMetadata(t *testing.T) {
	h := newGRPCHarness(t)

	tests := []struct {
		name string
		call func(context.Context, orderv1.OrderServiceClient) error
	}{
		{
			name: "create order requires auth",
			call: func(ctx context.Context, client orderv1.OrderServiceClient) error {
				_, err := client.CreateOrder(ctx, &orderv1.CreateOrderRequest{})
				return err
			},
		},
		{
			name: "get order requires auth",
			call: func(ctx context.Context, client orderv1.OrderServiceClient) error {
				_, err := client.GetOrder(ctx, &orderv1.GetOrderRequest{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(context.Background(), h.client)
			require.Error(t, err)
			require.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}

	require.False(t, h.verifier.called)
}

func TestOrderServerCreateOrder(t *testing.T) {
	claimsUserID := uuid.New()
	otherUserID := uuid.New()

	successOrder := outbound.Order{
		OrderID:     uuid.New(),
		UserID:      claimsUserID,
		Status:      outbound.OrderStatusAwaitingPayment,
		Currency:    "USD",
		TotalAmount: 1000,
		Items: []outbound.OrderItem{{
			OrderItemID: uuid.New(),
			ProductID:   uuid.New(),
			SKU:         "SKU-1",
			Name:        "Product",
			Quantity:    1,
			UnitPrice:   1000,
			LineTotal:   1000,
			Currency:    "USD",
		}},
	}

	tests := []struct {
		name         string
		ctx          context.Context
		request      *orderv1.CreateOrderRequest
		setupMock    func(m *mockCheckoutService)
		codeWant     codes.Code
		messageWant  string
		successCheck func(t *testing.T, resp *orderv1.CreateOrderResponse)
	}{
		{
			name: "auth required",
			ctx:  context.Background(),
			request: &orderv1.CreateOrderRequest{
				UserId:         claimsUserID.String(),
				IdempotencyKey: "idem-1",
			},
			codeWant:    codes.Unauthenticated,
			messageWant: "missing auth claims",
		},
		{
			name: "user mismatch",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.CreateOrderRequest{
				UserId:         otherUserID.String(),
				IdempotencyKey: "idem-2",
			},
			codeWant:    codes.PermissionDenied,
			messageWant: "request user id mismatch",
		},
		{
			name: "maps cart not found",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.CreateOrderRequest{
				UserId:         claimsUserID.String(),
				IdempotencyKey: "idem-3",
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, &checkout.CheckoutError{Code: checkout.CheckoutErrorCodeCartNotFound, Err: errors.New("cart")}).
					Once()
			},
			codeWant:    codes.NotFound,
			messageWant: string(checkout.CheckoutErrorCodeCartNotFound),
		},
		{
			name: "maps stock unavailable",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.CreateOrderRequest{
				UserId:         claimsUserID.String(),
				IdempotencyKey: "idem-4",
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, &checkout.CheckoutError{Code: checkout.CheckoutErrorCodeStockUnavailable, Err: errors.New("stock")}).
					Once()
			},
			codeWant:    codes.Aborted,
			messageWant: string(checkout.CheckoutErrorCodeStockUnavailable),
		},
		{
			name: "success",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.CreateOrderRequest{
				UserId:         claimsUserID.String(),
				IdempotencyKey: "idem-5",
				PaymentMethod:  "card",
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.MatchedBy(func(input checkout.CheckoutInput) bool {
					if input.UserID != claimsUserID {
						return false
					}

					if input.IdempotencyKey != "idem-5" {
						return false
					}

					return input.PaymentMethod != nil && *input.PaymentMethod == "card"
				})).
					Return(successOrder, nil).
					Once()
			},
			codeWant: codes.OK,
			successCheck: func(t *testing.T, resp *orderv1.CreateOrderResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.GetOrder())
				require.Equal(t, successOrder.OrderID.String(), resp.GetOrder().GetOrderId())
			},
		},
		{
			name: "trims idempotency key",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.CreateOrderRequest{
				UserId:         claimsUserID.String(),
				IdempotencyKey: "  idem-6  ",
				PaymentMethod:  "card",
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.MatchedBy(func(input checkout.CheckoutInput) bool {
					return input.IdempotencyKey == "idem-6"
				})).
					Return(successOrder, nil).
					Once()
			},
			codeWant: codes.OK,
			successCheck: func(t *testing.T, resp *orderv1.CreateOrderResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.GetOrder())
			},
		},
		{
			name: "whitespace idempotency key maps invalid argument",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.CreateOrderRequest{
				UserId:         claimsUserID.String(),
				IdempotencyKey: "   ",
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("Checkout", testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, &checkout.CheckoutError{Code: checkout.CheckoutErrorCodeInvalidArgument, Err: errors.New("idempotency key is required")}).
					Once()
			},
			codeWant:    codes.InvalidArgument,
			messageWant: string(checkout.CheckoutErrorCodeInvalidArgument),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkoutMock := newMockCheckoutService(t)
			if tt.setupMock != nil {
				tt.setupMock(checkoutMock)
			}

			server := NewOrderServer(checkoutMock, slog.New(slog.NewTextHandler(io.Discard, nil)))

			response, err := server.CreateOrder(tt.ctx, tt.request)
			if tt.codeWant == codes.OK {
				require.NoError(t, err)
				if tt.successCheck != nil {
					tt.successCheck(t, response)
				}
				return
			}

			require.Error(t, err)
			require.Equal(t, tt.codeWant, status.Code(err))
			require.Equal(t, tt.messageWant, status.Convert(err).Message())

			if tt.setupMock == nil {
				checkoutMock.AssertNotCalled(t, "Checkout", testifymock.Anything, testifymock.Anything)
			}
		})
	}
}

func TestOrderServerGetOrder(t *testing.T) {
	claimsUserID := uuid.New()
	otherUserID := uuid.New()
	orderID := uuid.New()

	successOrder := outbound.Order{
		OrderID:     orderID,
		UserID:      claimsUserID,
		Status:      outbound.OrderStatusAwaitingPayment,
		Currency:    "USD",
		TotalAmount: 1000,
		Items: []outbound.OrderItem{{
			OrderItemID: uuid.New(),
			ProductID:   uuid.New(),
			SKU:         "SKU-1",
			Name:        "Product",
			Quantity:    1,
			UnitPrice:   1000,
			LineTotal:   1000,
			Currency:    "USD",
		}},
	}

	tests := []struct {
		name         string
		ctx          context.Context
		request      *orderv1.GetOrderRequest
		setupMock    func(m *mockCheckoutService)
		codeWant     codes.Code
		messageWant  string
		successCheck func(t *testing.T, resp *orderv1.GetOrderResponse)
	}{
		{
			name: "auth required",
			ctx:  context.Background(),
			request: &orderv1.GetOrderRequest{
				UserId:  claimsUserID.String(),
				OrderId: orderID.String(),
			},
			codeWant:    codes.Unauthenticated,
			messageWant: "missing auth claims",
		},
		{
			name: "invalid user id",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.GetOrderRequest{
				UserId:  "  ",
				OrderId: orderID.String(),
			},
			codeWant:    codes.InvalidArgument,
			messageWant: "invalid user id",
		},
		{
			name: "ownership guard on request user id",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.GetOrderRequest{
				UserId:  otherUserID.String(),
				OrderId: orderID.String(),
			},
			codeWant:    codes.NotFound,
			messageWant: string(checkout.CheckoutErrorCodeCartNotFound),
		},
		{
			name: "invalid order id",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.GetOrderRequest{
				UserId:  claimsUserID.String(),
				OrderId: "",
			},
			codeWant:    codes.InvalidArgument,
			messageWant: "invalid order id",
		},
		{
			name: "maps not found",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.GetOrderRequest{
				UserId:  claimsUserID.String(),
				OrderId: orderID.String(),
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("GetOrder", testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, &checkout.CheckoutError{Code: checkout.CheckoutErrorCodeCartNotFound, Err: outbound.ErrOrderNotFound}).
					Once()
			},
			codeWant:    codes.NotFound,
			messageWant: string(checkout.CheckoutErrorCodeCartNotFound),
		},
		{
			name: "maps ownership mismatch as not found",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.GetOrderRequest{
				UserId:  claimsUserID.String(),
				OrderId: orderID.String(),
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("GetOrder", testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, outbound.ErrOrderNotFound).
					Once()
			},
			codeWant:    codes.NotFound,
			messageWant: string(checkout.CheckoutErrorCodeCartNotFound),
		},
		{
			name: "success",
			ctx:  withClaims(claimsUserID),
			request: &orderv1.GetOrderRequest{
				UserId:  claimsUserID.String(),
				OrderId: orderID.String(),
			},
			setupMock: func(m *mockCheckoutService) {
				m.On("GetOrder", testifymock.Anything, testifymock.MatchedBy(func(input checkout.GetOrderInput) bool {
					return input.UserID == claimsUserID && input.OrderID == orderID
				})).
					Return(successOrder, nil).
					Once()
			},
			codeWant: codes.OK,
			successCheck: func(t *testing.T, resp *orderv1.GetOrderResponse) {
				require.NotNil(t, resp)
				require.NotNil(t, resp.GetOrder())
				require.Equal(t, successOrder.OrderID.String(), resp.GetOrder().GetOrderId())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkoutMock := newMockCheckoutService(t)
			if tt.setupMock != nil {
				tt.setupMock(checkoutMock)
			}

			server := NewOrderServer(checkoutMock, slog.New(slog.NewTextHandler(io.Discard, nil)))

			response, err := server.GetOrder(tt.ctx, tt.request)
			if tt.codeWant == codes.OK {
				require.NoError(t, err)
				if tt.successCheck != nil {
					tt.successCheck(t, response)
				}
				return
			}

			require.Error(t, err)
			require.Equal(t, tt.codeWant, status.Code(err))
			require.Equal(t, tt.messageWant, status.Convert(err).Message())

			if tt.setupMock == nil {
				checkoutMock.AssertNotCalled(t, "GetOrder", testifymock.Anything, testifymock.Anything)
			}
		})
	}
}

type grpcHarness struct {
	client   orderv1.OrderServiceClient
	verifier *testTokenVerifier
}

func newGRPCHarness(t *testing.T) *grpcHarness {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	verifier := &testTokenVerifier{claims: transport.Claims{UserID: uuid.New()}}
	server := NewServer(logger, "order-svc-test", nil, verifier, noop.NewTracerProvider().Tracer("order-svc-test"))

	listener := bufconn.Listen(1024 * 1024)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.Dial()
	}

	conn, err := grpcpkg.NewClient(
		"passthrough:///order-svc-test",
		grpcpkg.WithContextDialer(dialer),
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
	})

	return &grpcHarness{
		client:   orderv1.NewOrderServiceClient(conn),
		verifier: verifier,
	}
}

type tokenVerifierStub struct {
	err error
}

func (s tokenVerifierStub) Verify(_ string) (transport.Claims, error) {
	if s.err != nil {
		return transport.Claims{}, s.err
	}

	return transport.Claims{}, nil
}

type testTokenVerifier struct {
	claims transport.Claims
	calls  int
	called bool
	err    error
}

func (s *testTokenVerifier) Verify(_ string) (transport.Claims, error) {
	s.calls++
	s.called = true
	if s.err != nil {
		return transport.Claims{}, s.err
	}

	return s.claims, nil
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

func (m *mockCheckoutService) GetOrder(ctx context.Context, input checkout.GetOrderInput) (outbound.Order, error) {
	args := m.Called(ctx, input)

	order, _ := args.Get(0).(outbound.Order)
	return order, args.Error(1)
}

func withClaims(userID uuid.UUID) context.Context {
	return transport.WithClaims(context.Background(), transport.Claims{UserID: userID})
}

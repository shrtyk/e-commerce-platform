package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

func TestCartServerGetActiveCart(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name         string
		ctx          context.Context
		request      *cartv1.GetActiveCartRequest
		setup        func(*fakeCartService)
		expectedCode codes.Code
	}{
		{
			name:         "missing auth claims",
			ctx:          context.Background(),
			request:      &cartv1.GetActiveCartRequest{UserId: userID.String()},
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "invalid user id",
			ctx:          withClaims(userID),
			request:      &cartv1.GetActiveCartRequest{UserId: "bad-uuid"},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "request user mismatch",
			ctx:          withClaims(userID),
			request:      &cartv1.GetActiveCartRequest{UserId: uuid.NewString()},
			expectedCode: codes.PermissionDenied,
		},
		{
			name:    "cart not found",
			ctx:     withClaims(userID),
			request: &cartv1.GetActiveCartRequest{UserId: userID.String()},
			setup: func(svc *fakeCartService) {
				svc.getActiveCartFn = func(context.Context, uuid.UUID) (domain.Cart, error) {
					return domain.Cart{}, cart.ErrCartNotFound
				}
			},
			expectedCode: codes.NotFound,
		},
		{
			name:    "success",
			ctx:     withClaims(userID),
			request: &cartv1.GetActiveCartRequest{UserId: userID.String()},
			setup: func(svc *fakeCartService) {
				svc.getActiveCartFn = func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, error) {
					require.Equal(t, userID, gotUserID)
					return testDomainCart(userID), nil
				}
			},
			expectedCode: codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeCartService{}
			if tt.setup != nil {
				tt.setup(svc)
			}

			server := NewCartServer(svc, newTestLogger())
			response, err := server.GetActiveCart(tt.ctx, tt.request)
			requireCode(t, err, tt.expectedCode)

			if tt.expectedCode != codes.OK {
				require.Nil(t, response)
				return
			}

			require.NotNil(t, response)
			require.Equal(t, userID.String(), response.GetCart().GetUserId())
			require.Equal(t, int64(2), response.GetCart().GetItems()[0].GetQuantity())
			require.Equal(t, int64(1000), response.GetCart().GetItems()[0].GetUnitPrice().GetAmount())
		})
	}
}

func TestCartServerMutatingMethods(t *testing.T) {
	tests := []struct {
		name         string
		call         func(context.Context, *CartServer, string) error
		setup        func(*fakeCartService, uuid.UUID)
		expectedCode codes.Code
	}{
		{
			name: "add item invalid quantity",
			setup: func(svc *fakeCartService, userID uuid.UUID) {
				svc.addCartItemFn = func(_ context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					return domain.Cart{}, cart.ErrInvalidQuantity
				}
			},
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.AddCartItem(ctx, &cartv1.AddCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 0})
				return err
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "add item conflict",
			setup: func(svc *fakeCartService, userID uuid.UUID) {
				svc.addCartItemFn = func(_ context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					return domain.Cart{}, cart.ErrCartItemAlreadyExists
				}
			},
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.AddCartItem(ctx, &cartv1.AddCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 1})
				return err
			},
			expectedCode: codes.AlreadyExists,
		},
		{
			name: "add item product snapshot not found",
			setup: func(svc *fakeCartService, userID uuid.UUID) {
				svc.addCartItemFn = func(_ context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					return domain.Cart{}, cart.ErrProductSnapshotNotFound
				}
			},
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.AddCartItem(ctx, &cartv1.AddCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 1})
				return err
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "add item currency mismatch",
			setup: func(svc *fakeCartService, userID uuid.UUID) {
				svc.addCartItemFn = func(_ context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					return domain.Cart{}, cart.ErrCartCurrencyMismatch
				}
			},
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.AddCartItem(ctx, &cartv1.AddCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 1})
				return err
			},
			expectedCode: codes.FailedPrecondition,
		},
		{
			name: "update item not found",
			setup: func(svc *fakeCartService, userID uuid.UUID) {
				svc.updateCartItemFn = func(_ context.Context, input cart.UpdateCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					return domain.Cart{}, cart.ErrCartItemNotFound
				}
			},
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.UpdateCartItem(ctx, &cartv1.UpdateCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 2})
				return err
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "remove item internal",
			setup: func(svc *fakeCartService, userID uuid.UUID) {
				svc.removeCartItemFn = func(_ context.Context, input cart.RemoveCartItemInput) (domain.Cart, error) {
					require.Equal(t, userID, input.UserID)
					return domain.Cart{}, errors.New("storage down")
				}
			},
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.RemoveCartItem(ctx, &cartv1.RemoveCartItemRequest{UserId: userID, Sku: "SKU-1"})
				return err
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			svc := &fakeCartService{}
			if tt.setup != nil {
				tt.setup(svc, userID)
			}

			server := NewCartServer(svc, newTestLogger())
			err := tt.call(withClaims(userID), server, userID.String())
			requireCode(t, err, tt.expectedCode)
		})
	}
}

func TestCartServerMutatingMethodsUserIDMismatch(t *testing.T) {
	claimedUserID := uuid.New()
	requestedUserID := uuid.NewString()

	tests := []struct {
		name string
		call func(context.Context, *CartServer, string) error
	}{
		{
			name: "add item",
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.AddCartItem(ctx, &cartv1.AddCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 1})
				return err
			},
		},
		{
			name: "update item",
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.UpdateCartItem(ctx, &cartv1.UpdateCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 2})
				return err
			},
		},
		{
			name: "remove item",
			call: func(ctx context.Context, server *CartServer, userID string) error {
				_, err := server.RemoveCartItem(ctx, &cartv1.RemoveCartItemRequest{UserId: userID, Sku: "SKU-1"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewCartServer(&fakeCartService{}, newTestLogger())
			err := tt.call(withClaims(claimedUserID), server, requestedUserID)
			requireCode(t, err, codes.PermissionDenied)
		})
	}
}

func TestGetCheckoutSnapshot(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name         string
		ctx          context.Context
		request      *cartv1.GetCheckoutSnapshotRequest
		setup        func(*fakeCartService)
		expectedCode codes.Code
	}{
		{
			name:         "missing auth claims",
			ctx:          context.Background(),
			request:      &cartv1.GetCheckoutSnapshotRequest{UserId: userID.String()},
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "invalid user id",
			ctx:          withClaims(userID),
			request:      &cartv1.GetCheckoutSnapshotRequest{UserId: "bad-uuid"},
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "request user mismatch",
			ctx:          withClaims(userID),
			request:      &cartv1.GetCheckoutSnapshotRequest{UserId: uuid.NewString()},
			expectedCode: codes.PermissionDenied,
		},
		{
			name:    "checkout product missing",
			ctx:     withClaims(userID),
			request: &cartv1.GetCheckoutSnapshotRequest{UserId: userID.String()},
			setup: func(svc *fakeCartService) {
				svc.getCheckoutSnapshotFn = func(context.Context, uuid.UUID) (domain.Cart, error) {
					return domain.Cart{}, cart.ErrProductSnapshotNotFound
				}
			},
			expectedCode: codes.NotFound,
		},
		{
			name:    "checkout unexpected internal error",
			ctx:     withClaims(userID),
			request: &cartv1.GetCheckoutSnapshotRequest{UserId: userID.String()},
			setup: func(svc *fakeCartService) {
				svc.getCheckoutSnapshotFn = func(context.Context, uuid.UUID) (domain.Cart, error) {
					return domain.Cart{}, errors.New("storage down")
				}
			},
			expectedCode: codes.Internal,
		},
		{
			name:    "success",
			ctx:     withClaims(userID),
			request: &cartv1.GetCheckoutSnapshotRequest{UserId: userID.String()},
			setup: func(svc *fakeCartService) {
				svc.getCheckoutSnapshotFn = func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, error) {
					require.Equal(t, userID, gotUserID)
					return testDomainCart(userID), nil
				}
			},
			expectedCode: codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeCartService{}
			if tt.setup != nil {
				tt.setup(svc)
			}

			server := NewCartServer(svc, newTestLogger())
			response, err := server.GetCheckoutSnapshot(tt.ctx, tt.request)
			requireCode(t, err, tt.expectedCode)

			if tt.expectedCode != codes.OK {
				require.Nil(t, response)
				return
			}

			require.NotNil(t, response)
			require.Equal(t, userID.String(), response.GetSnapshot().GetUserId())
			require.Equal(t, int64(2000), response.GetSnapshot().GetTotalAmount().GetAmount())
			require.Len(t, response.GetSnapshot().GetItems(), 1)
			require.Equal(t, "SKU-1", response.GetSnapshot().GetItems()[0].GetSku())
			require.Equal(t, "Product", response.GetSnapshot().GetItems()[0].GetName())
			require.Equal(t, int64(1000), response.GetSnapshot().GetItems()[0].GetUnitPrice().GetAmount())
			require.Equal(t, int64(2000), response.GetSnapshot().GetItems()[0].GetLineTotal().GetAmount())
		})
	}
}

func TestCartServerRemoveCartItemInternalErrorLogsEnrichedFields(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	userID := uuid.New()
	requestID := "req-cart-1"
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		SpanID:     trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2},
		TraceFlags: trace.FlagsSampled,
	})

	ctx := withClaims(userID)
	ctx = transport.WithRequestID(ctx, requestID)
	ctx = trace.ContextWithSpanContext(ctx, spanContext)

	server := NewCartServer(&fakeCartService{
		removeCartItemFn: func(context.Context, cart.RemoveCartItemInput) (domain.Cart, error) {
			return domain.Cart{}, errors.New("storage down")
		},
	}, logger)

	response, err := server.RemoveCartItem(ctx, &cartv1.RemoveCartItemRequest{UserId: userID.String(), Sku: "SKU-1"})
	require.Nil(t, response)
	requireCode(t, err, codes.Internal)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))

	require.Equal(t, "grpc internal error", entry["msg"])
	require.Equal(t, "cart-svc", entry["service"])
	require.Equal(t, requestID, entry["request_id"])
	require.Equal(t, spanContext.TraceID().String(), entry["trace_id"])
	require.Equal(t, "RemoveCartItem", entry["method"])
	require.Equal(t, cartv1.CartService_RemoveCartItem_FullMethodName, entry["path"])
	require.Equal(t, float64(codes.Internal), entry["status"])
	require.Equal(t, codes.Internal.String(), entry["grpc_status"])
	require.Equal(t, "storage down", entry["error"])
	require.Equal(t, userID.String(), entry["user_id"])
	require.Equal(t, "SKU-1", entry["sku"])
}

func TestCartServerRemoveCartItemInternalErrorLogsFallbackMetadata(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	userID := uuid.New()
	ctx := transport.WithClaims(context.Background(), transport.Claims{UserID: userID, Role: "user", Status: "active"})

	server := NewCartServer(&fakeCartService{
		removeCartItemFn: func(context.Context, cart.RemoveCartItemInput) (domain.Cart, error) {
			return domain.Cart{}, errors.New("storage down")
		},
	}, logger)

	require.NotPanics(t, func() {
		response, err := server.RemoveCartItem(ctx, &cartv1.RemoveCartItemRequest{UserId: userID.String(), Sku: "SKU-1"})
		require.Nil(t, response)
		requireCode(t, err, codes.Internal)
	})

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))

	require.Equal(t, "", entry["request_id"])
	require.Equal(t, "", entry["trace_id"])
	require.Equal(t, codes.Internal.String(), entry["grpc_status"])
}

type fakeCartService struct {
	getActiveCartFn       func(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
	addCartItemFn         func(ctx context.Context, input cart.AddCartItemInput) (domain.Cart, error)
	updateCartItemFn      func(ctx context.Context, input cart.UpdateCartItemInput) (domain.Cart, error)
	removeCartItemFn      func(ctx context.Context, input cart.RemoveCartItemInput) (domain.Cart, error)
	getCheckoutSnapshotFn func(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
}

func (s *fakeCartService) GetActiveCart(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	if s.getActiveCartFn == nil {
		return domain.Cart{}, nil
	}

	return s.getActiveCartFn(ctx, userID)
}

func (s *fakeCartService) AddCartItem(ctx context.Context, input cart.AddCartItemInput) (domain.Cart, error) {
	if s.addCartItemFn == nil {
		return domain.Cart{}, nil
	}

	return s.addCartItemFn(ctx, input)
}

func (s *fakeCartService) UpdateCartItem(ctx context.Context, input cart.UpdateCartItemInput) (domain.Cart, error) {
	if s.updateCartItemFn == nil {
		return domain.Cart{}, nil
	}

	return s.updateCartItemFn(ctx, input)
}

func (s *fakeCartService) RemoveCartItem(ctx context.Context, input cart.RemoveCartItemInput) (domain.Cart, error) {
	if s.removeCartItemFn == nil {
		return domain.Cart{}, nil
	}

	return s.removeCartItemFn(ctx, input)
}

func (s *fakeCartService) GetCheckoutSnapshot(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	if s.getCheckoutSnapshotFn == nil {
		return domain.Cart{}, nil
	}

	return s.getCheckoutSnapshotFn(ctx, userID)
}

func withClaims(userID uuid.UUID) context.Context {
	return transport.WithClaims(context.Background(), transport.Claims{UserID: userID, Role: "user", Status: "active"})
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func requireCode(t *testing.T, err error, expected codes.Code) {
	t.Helper()

	if expected == codes.OK {
		require.NoError(t, err)
		return
	}

	require.Error(t, err)
	require.Equal(t, expected, status.Code(err))
}

func testDomainCart(userID uuid.UUID) domain.Cart {
	return domain.Cart{
		ID:          uuid.New(),
		UserID:      userID,
		Status:      domain.CartStatusActive,
		Currency:    "USD",
		TotalAmount: 2000,
		Items:       []domain.CartItem{testDomainItem("SKU-1", 2, 1000)},
	}
}

func testDomainItem(sku string, quantity int64, unitPrice int64) domain.CartItem {
	return domain.CartItem{
		SKU:       sku,
		Name:      "Product",
		Quantity:  quantity,
		UnitPrice: unitPrice,
		Currency:  "USD",
		LineTotal: quantity * unitPrice,
	}
}

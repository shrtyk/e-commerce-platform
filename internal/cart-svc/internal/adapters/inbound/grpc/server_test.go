package grpc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
)

func TestServerAuthInterceptor(t *testing.T) {
	tests := []struct {
		name          string
		call          func(context.Context, cartv1.CartServiceClient, string) error
		expectedCode  codes.Code
		wantVerifyHit bool
	}{
		{
			name: "protected get active cart requires auth",
			call: func(ctx context.Context, client cartv1.CartServiceClient, userID string) error {
				_, err := client.GetActiveCart(ctx, &cartv1.GetActiveCartRequest{UserId: userID})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "protected add cart item requires auth",
			call: func(ctx context.Context, client cartv1.CartServiceClient, userID string) error {
				_, err := client.AddCartItem(ctx, &cartv1.AddCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 1})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "protected update cart item requires auth",
			call: func(ctx context.Context, client cartv1.CartServiceClient, userID string) error {
				_, err := client.UpdateCartItem(ctx, &cartv1.UpdateCartItemRequest{UserId: userID, Sku: "SKU-1", Quantity: 1})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "protected remove cart item requires auth",
			call: func(ctx context.Context, client cartv1.CartServiceClient, userID string) error {
				_, err := client.RemoveCartItem(ctx, &cartv1.RemoveCartItemRequest{UserId: userID, Sku: "SKU-1"})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "public checkout snapshot bypasses auth and stays unimplemented",
			call: func(ctx context.Context, client cartv1.CartServiceClient, _ string) error {
				_, err := client.GetCheckoutSnapshot(ctx, &cartv1.GetCheckoutSnapshotRequest{})
				return err
			},
			expectedCode:  codes.Unimplemented,
			wantVerifyHit: false,
		},
		{
			name: "protected method with auth calls verifier",
			call: func(_ context.Context, client cartv1.CartServiceClient, userID string) error {
				ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer valid-token")
				_, err := client.GetActiveCart(ctx, &cartv1.GetActiveCartRequest{UserId: userID})
				return err
			},
			expectedCode:  codes.OK,
			wantVerifyHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newGRPCHarness(t)

			err := tt.call(context.Background(), h.client, h.userID.String())
			if tt.expectedCode == codes.OK {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tt.expectedCode, status.Code(err))
			}

			require.Equal(t, tt.wantVerifyHit, h.verifier.called)
		})
	}
}

type grpcHarness struct {
	client   cartv1.CartServiceClient
	verifier *testTokenVerifier
	userID   uuid.UUID
}

func newGRPCHarness(t *testing.T) *grpcHarness {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	verifier := &testTokenVerifier{}

	userID := uuid.New()
	claims := transport.Claims{UserID: userID, Role: "user", Status: "active"}
	verifier.verify = func(string) (transport.Claims, error) {
		return claims, nil
	}

	service := &fakeCartService{
		getActiveCartFn: func(context.Context, uuid.UUID) (domain.Cart, error) {
			return testDomainCart(userID), nil
		},
		addCartItemFn: func(context.Context, cart.AddCartItemInput) (domain.Cart, error) {
			return testDomainCart(userID), nil
		},
		updateCartItemFn: func(context.Context, cart.UpdateCartItemInput) (domain.Cart, error) {
			return testDomainCart(userID), nil
		},
		removeCartItemFn: func(context.Context, cart.RemoveCartItemInput) (domain.Cart, error) {
			return testDomainCart(userID), nil
		},
	}

	server := NewServer(logger, "cart-svc-test", service, verifier, noop.NewTracerProvider().Tracer("cart-svc-test"))

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
		"passthrough:///cart-svc-test",
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
		client:   cartv1.NewCartServiceClient(conn),
		verifier: verifier,
		userID:   userID,
	}
}

type testTokenVerifier struct {
	verify func(token string) (transport.Claims, error)
	called bool
}

var _ httpcommon.TokenVerifier = (*testTokenVerifier)(nil)

func (v *testTokenVerifier) Verify(token string) (transport.Claims, error) {
	v.called = true
	if v.verify == nil {
		return transport.Claims{}, nil
	}

	return v.verify(token)
}

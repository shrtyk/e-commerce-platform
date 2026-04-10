package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/common/auth"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type testTokenVerifier struct {
	verify func(token string) (auth.Claims, error)
	called bool
	last   string
}

func (v *testTokenVerifier) Verify(token string) (auth.Claims, error) {
	v.called = true
	v.last = token

	if v.verify == nil {
		return auth.Claims{}, nil
	}

	return v.verify(token)
}

func TestUnaryAuthInterceptor(t *testing.T) {
	validClaims := auth.Claims{
		UserID: uuid.New(),
		Role:   auth.RoleUser,
		Status: auth.StatusActive,
	}

	tests := []struct {
		name          string
		method        string
		header        string
		incomingMD    metadata.MD
		publicMethods []string
		requiredRoles []auth.Role
		verifier      *testTokenVerifier
		handler       grpcpkg.UnaryHandler
		wantCode      codes.Code
		wantVerifier  bool
		wantToken     string
	}{
		{
			name:          "public method bypass",
			method:        "/catalog.v1.ProductService/ListProducts",
			publicMethods: []string{"/catalog.v1.ProductService/ListProducts"},
			verifier: &testTokenVerifier{
				verify: func(token string) (auth.Claims, error) {
					return auth.Claims{}, errors.New("must not be called")
				},
			},
			handler:      func(context.Context, interface{}) (interface{}, error) { return "ok", nil },
			wantCode:     codes.OK,
			wantVerifier: false,
		},
		{
			name:         "missing metadata header",
			method:       "/orders.v1.OrderService/CreateOrder",
			verifier:     &testTokenVerifier{},
			handler:      func(context.Context, interface{}) (interface{}, error) { return "ok", nil },
			wantCode:     codes.Unauthenticated,
			wantVerifier: false,
		},
		{
			name:         "metadata present without authorization",
			method:       "/orders.v1.OrderService/CreateOrder",
			incomingMD:   metadata.Pairs("x-request-id", "req-1"),
			verifier:     &testTokenVerifier{},
			handler:      func(context.Context, interface{}) (interface{}, error) { return "ok", nil },
			wantCode:     codes.Unauthenticated,
			wantVerifier: false,
		},
		{
			name:         "malformed bearer",
			method:       "/orders.v1.OrderService/CreateOrder",
			header:       "Bearer",
			verifier:     &testTokenVerifier{},
			handler:      func(context.Context, interface{}) (interface{}, error) { return "ok", nil },
			wantCode:     codes.Unauthenticated,
			wantVerifier: false,
		},
		{
			name:   "verifier error",
			method: "/orders.v1.OrderService/CreateOrder",
			header: "Bearer bad-token",
			verifier: &testTokenVerifier{verify: func(token string) (auth.Claims, error) {
				return auth.Claims{}, errors.New("verify failed")
			}},
			handler:      func(context.Context, interface{}) (interface{}, error) { return "ok", nil },
			wantCode:     codes.Unauthenticated,
			wantVerifier: true,
			wantToken:    "bad-token",
		},
		{
			name:   "invalid claims",
			method: "/orders.v1.OrderService/CreateOrder",
			header: "Bearer valid-token",
			verifier: &testTokenVerifier{verify: func(token string) (auth.Claims, error) {
				return auth.Claims{UserID: uuid.New(), Role: auth.RoleUnknown, Status: auth.StatusActive}, nil
			}},
			handler:      func(context.Context, interface{}) (interface{}, error) { return "ok", nil },
			wantCode:     codes.Unauthenticated,
			wantVerifier: true,
			wantToken:    "valid-token",
		},
		{
			name:          "role mismatch",
			method:        "/orders.v1.OrderService/CreateOrder",
			header:        "Bearer valid-token",
			requiredRoles: []auth.Role{auth.RoleAdmin},
			verifier: &testTokenVerifier{verify: func(token string) (auth.Claims, error) {
				return validClaims, nil
			}},
			handler:      func(context.Context, interface{}) (interface{}, error) { return "ok", nil },
			wantCode:     codes.PermissionDenied,
			wantVerifier: true,
			wantToken:    "valid-token",
		},
		{
			name:          "success claims in context",
			method:        "/orders.v1.OrderService/CreateOrder",
			header:        "Bearer valid-token",
			requiredRoles: []auth.Role{auth.RoleUser},
			verifier: &testTokenVerifier{verify: func(token string) (auth.Claims, error) {
				return validClaims, nil
			}},
			handler: func(ctx context.Context, req interface{}) (interface{}, error) {
				claims, ok := transport.ClaimsFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, validClaims.UserID, claims.UserID)
				require.Equal(t, string(validClaims.Role), claims.Role)
				require.Equal(t, string(validClaims.Status), claims.Status)
				return "ok", nil
			},
			wantCode:     codes.OK,
			wantVerifier: true,
			wantToken:    "valid-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := auth.UnaryAuthInterceptor(tt.verifier, func(ctx context.Context, claims auth.Claims) context.Context {
				return transport.WithClaims(ctx, transport.Claims{
					UserID: claims.UserID,
					Role:   string(claims.Role),
					Status: string(claims.Status),
				})
			}, tt.publicMethods, tt.requiredRoles...)

			ctx := context.Background()
			if tt.incomingMD != nil {
				ctx = metadata.NewIncomingContext(ctx, tt.incomingMD)
			} else if tt.header != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", tt.header))
			}

			resp, err := interceptor(ctx, "req", &grpcpkg.UnaryServerInfo{FullMethod: tt.method}, tt.handler)
			if tt.wantCode == codes.OK {
				require.NoError(t, err)
				require.Equal(t, "ok", resp)
			} else {
				require.Error(t, err)
				require.Equal(t, tt.wantCode, status.Code(err))
			}

			require.Equal(t, tt.wantVerifier, tt.verifier.called)
			if tt.wantVerifier {
				require.Equal(t, tt.wantToken, tt.verifier.last)
			}
		})
	}
}

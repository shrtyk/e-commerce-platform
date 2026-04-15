package grpc

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"

	commonauth "github.com/shrtyk/e-commerce-platform/internal/common/auth"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
)

var publicMethods = []string{
	cartv1.CartService_GetActiveCart_FullMethodName,
	cartv1.CartService_AddCartItem_FullMethodName,
	cartv1.CartService_UpdateCartItem_FullMethodName,
	cartv1.CartService_RemoveCartItem_FullMethodName,
	cartv1.CartService_GetCheckoutSnapshot_FullMethodName,
}

func NewServer(
	logger *slog.Logger,
	serviceName string,
	tokenVerifier httpcommon.TokenVerifier,
	tracer trace.Tracer,
) *grpcpkg.Server {
	interceptorsProvider := grpccommon.NewInterceptorsProviderWithTracer(serviceName, logger, tracer)

	server := grpcpkg.NewServer(
		grpcpkg.ChainUnaryInterceptor(
			interceptorsProvider.UnaryTracing(),
			interceptorsProvider.UnaryLogging(),
			interceptorsProvider.UnaryRecovery(),
			commonauth.UnaryAuthInterceptor(
				newGRPCTokenVerifier(tokenVerifier),
				func(ctx context.Context, claims commonauth.Claims) context.Context {
					return transport.WithClaims(ctx, transport.Claims{
						UserID: claims.UserID,
						Role:   string(claims.Role),
						Status: string(claims.Status),
					})
				},
				publicMethods,
			),
		),
	)

	cartv1.RegisterCartServiceServer(server, NewCartServer(logger))

	return server
}

type grpcTokenVerifier struct {
	tokenVerifier httpcommon.TokenVerifier
}

func newGRPCTokenVerifier(tokenVerifier httpcommon.TokenVerifier) commonauth.TokenVerifier {
	if tokenVerifier == nil {
		return nil
	}

	return grpcTokenVerifier{tokenVerifier: tokenVerifier}
}

func (v grpcTokenVerifier) Verify(token string) (commonauth.Claims, error) {
	claims, err := v.tokenVerifier.Verify(token)
	if err != nil {
		return commonauth.Claims{}, err
	}

	return transport.ToAuthClaims(claims)
}

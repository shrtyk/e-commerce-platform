package grpc

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"

	commonauth "github.com/shrtyk/e-commerce-platform/internal/common/auth"
	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

var publicMethods = []string{
	"/ecommerce.identity.v1.IdentityService/RegisterUser",
	"/ecommerce.identity.v1.IdentityService/LoginUser",
	"/ecommerce.identity.v1.IdentityService/RefreshToken",
}

func NewServer(
	logger *slog.Logger,
	serviceName string,
	authService *auth.AuthService,
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

	identityv1.RegisterIdentityServiceServer(server, NewIdentityServer(authService, logger))

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

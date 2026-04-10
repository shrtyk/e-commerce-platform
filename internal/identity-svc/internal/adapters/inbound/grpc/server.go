package grpc

import (
	"log/slog"

	grpcpkg "google.golang.org/grpc"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

func NewServer(logger *slog.Logger, serviceName string, authService *auth.AuthService) *grpcpkg.Server {
	server := grpcpkg.NewServer(
		grpcpkg.ChainUnaryInterceptor(
			grpccommon.UnaryLogging(logger, serviceName),
			grpccommon.UnaryRecovery(logger),
		),
	)

	identityv1.RegisterIdentityServiceServer(server, NewIdentityServer(authService, logger))

	return server
}

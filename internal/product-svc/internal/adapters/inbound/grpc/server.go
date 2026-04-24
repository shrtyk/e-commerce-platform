package grpc

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
)

func NewServer(
	logger *slog.Logger,
	serviceName string,
	catalogService catalogService,
	defaultPageSize int32,
	tracer trace.Tracer,
) *grpcpkg.Server {
	interceptorsProvider := grpccommon.NewInterceptorsProviderWithTracer(serviceName, logger, tracer)

	server := grpcpkg.NewServer(
		grpcpkg.ChainUnaryInterceptor(
			interceptorsProvider.UnaryTracing(),
			interceptorsProvider.UnaryLogging(),
			interceptorsProvider.UnaryRecovery(),
		),
	)

	catalogv1.RegisterCatalogServiceServer(server, NewCatalogServer(catalogService, logger, defaultPageSize))

	return server
}

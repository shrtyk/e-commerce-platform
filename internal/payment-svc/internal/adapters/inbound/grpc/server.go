package grpc

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"

	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
)

func NewServer(
	logger *slog.Logger,
	serviceName string,
	paymentService paymentService,
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

	paymentv1.RegisterPaymentServiceServer(server, NewPaymentServer(paymentService, logger))

	return server
}

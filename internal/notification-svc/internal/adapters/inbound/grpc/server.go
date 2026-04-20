package grpc

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"

	notificationv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/notification/v1"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
)

func NewServer(
	logger *slog.Logger,
	serviceName string,
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

	notificationv1.RegisterNotificationServiceServer(server, NewNotificationServer(logger))

	return server
}

type NotificationServer struct {
	notificationv1.UnimplementedNotificationServiceServer
}

func NewNotificationServer(_ *slog.Logger) *NotificationServer {
	return &NotificationServer{}
}

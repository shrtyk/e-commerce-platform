package grpc

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryLogging(logger *slog.Logger, serviceName string) grpcpkg.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpcpkg.UnaryServerInfo, handler grpcpkg.UnaryHandler) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start).Milliseconds()
		requestID := transport.RequestIDFromContext(ctx)
		statusCode := status.Code(err)

		logger.Info("request",
			slog.String("service", serviceName),
			slog.String("path", info.FullMethod),
			slog.Int("status", int(statusCode)),
			slog.String("grpc_status", statusCode.String()),
			slog.Int64("duration_ms", duration),
			slog.String("request_id", requestID),
		)

		return resp, err
	}
}

func UnaryRecovery(logger *slog.Logger) grpcpkg.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpcpkg.UnaryServerInfo, handler grpcpkg.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID := transport.RequestIDFromContext(ctx)
				logger.Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("request_id", requestID),
					slog.String("stack", string(debug.Stack())),
				)
				err = status.Error(codes.Internal, "internal server error")
				resp = nil
			}
		}()

		return handler(ctx, req)
	}
}

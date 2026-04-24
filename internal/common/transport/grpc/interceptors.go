package grpc

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type InterceptorsProvider struct {
	serviceName string
	logger      *slog.Logger
	tracer      trace.Tracer
}

func NewInterceptorsProvider(serviceName string, logger *slog.Logger) *InterceptorsProvider {
	tracer := noop.NewTracerProvider().Tracer(serviceName)

	return NewInterceptorsProviderWithTracer(serviceName, logger, tracer)
}

func NewInterceptorsProviderWithTracer(serviceName string, logger *slog.Logger, tracer trace.Tracer) *InterceptorsProvider {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer(serviceName)
	}

	return &InterceptorsProvider{
		serviceName: serviceName,
		logger:      logger,
		tracer:      tracer,
	}
}

func (p *InterceptorsProvider) UnaryTracing() grpcpkg.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpcpkg.UnaryServerInfo, handler grpcpkg.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			ctx = observability.ExtractGRPCMetadata(ctx, md)
		}

		ctx, span := p.tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		serviceName, methodName := parseRPCMethod(info.FullMethod)
		span.SetAttributes(
			attribute.String("rpc.system", "grpc"),
			attribute.String("rpc.method", methodName),
			attribute.String("rpc.service", serviceName),
		)

		resp, err := handler(ctx, req)

		grpcStatusCode := status.Code(err)
		span.SetAttributes(attribute.String("rpc.grpc.status_code", strconv.Itoa(int(grpcStatusCode))))

		if grpcStatusCode == grpccodes.OK {
			span.SetStatus(otelcodes.Ok, grpcStatusCode.String())
		} else if grpcStatusCode >= grpccodes.Internal {
			span.SetStatus(otelcodes.Error, grpcStatusCode.String())
		}

		return resp, err
	}
}

func (p *InterceptorsProvider) UnaryLogging() grpcpkg.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpcpkg.UnaryServerInfo, handler grpcpkg.UnaryHandler) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start).Milliseconds()
		requestID := transport.RequestIDFromContext(ctx)
		statusCode := status.Code(err)

		p.logger.Info("request",
			slog.String("service", p.serviceName),
			slog.String("path", info.FullMethod),
			slog.Int("status", int(statusCode)),
			slog.String("grpc_status", statusCode.String()),
			slog.Int64("duration_ms", duration),
			slog.String("request_id", requestID),
		)

		return resp, err
	}
}

func (p *InterceptorsProvider) UnaryRecovery() grpcpkg.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpcpkg.UnaryServerInfo, handler grpcpkg.UnaryHandler) (resp any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID := transport.RequestIDFromContext(ctx)
				p.logger.Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("request_id", requestID),
					slog.String("stack", string(debug.Stack())),
				)
				err = status.Error(grpccodes.Internal, "internal server error")
				resp = nil
			}
		}()

		return handler(ctx, req)
	}
}

func parseRPCMethod(fullMethod string) (service string, method string) {
	parts := strings.Split(strings.TrimPrefix(fullMethod, "/"), "/")
	if len(parts) != 2 {
		return "", ""
	}

	return parts[0], parts[1]
}

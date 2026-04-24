package grpc

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"go.opentelemetry.io/otel"
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
	requestMetrics *observability.RequestMetrics
}

func NewInterceptorsProvider(serviceName string, logger *slog.Logger) *InterceptorsProvider {
	tracer := noop.NewTracerProvider().Tracer(serviceName)

	return NewInterceptorsProviderWithTracer(serviceName, logger, tracer)
}

func NewInterceptorsProviderWithTracer(serviceName string, logger *slog.Logger, tracer trace.Tracer) *InterceptorsProvider {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer(serviceName)
	}

	requestMetrics := initRequestMetrics("internal/common/transport/grpc", logger)

	return &InterceptorsProvider{
		serviceName: serviceName,
		logger:      logger,
		tracer:      tracer,
		requestMetrics: requestMetrics,
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
		operation := grpcOperationLabel(info.FullMethod)

		var resp any
		var err error

		defer func() {
			recovered := recover()

			statusCode := status.Code(err)
			if recovered != nil {
				statusCode = grpccodes.Internal
				err = status.Error(grpccodes.Internal, "internal server error")
			}

			duration := time.Since(start)
			requestID := transport.RequestIDFromContext(ctx)
			traceID := logging.TraceIDFromContext(ctx)
			_, methodName := parseRPCMethod(info.FullMethod)

			requestFields := logging.RequestFields(
				p.serviceName,
				requestID,
				traceID,
				methodName,
				info.FullMethod,
				int(statusCode),
				duration.Milliseconds(),
			)

			p.logger.Info("request", append(requestFields,
				slog.String(logging.FieldGRPCStatus, statusCode.String()),
			)...)

			p.requestMetrics.Record(ctx, duration, observability.RequestMetricAttrs{
				Transport: "grpc",
				Operation: operation,
				Status:    grpcMetricStatus(statusCode, err, ctx, recovered),
				Outcome:   grpcMetricOutcome(statusCode, err, ctx, recovered),
			})

			if recovered != nil {
				panic(recovered)
			}
		}()

		resp, err = handler(ctx, req)

		return resp, err
	}
}

func initRequestMetrics(meterName string, logger *slog.Logger) *observability.RequestMetrics {
	requestMetrics, err := observability.NewRequestMetrics(otel.GetMeterProvider().Meter(meterName))
	if err != nil {
		if logger != nil {
			logger.Error("metrics init failed",
				slog.String("component", "grpc.interceptors"),
				slog.String("metric", observability.MetricNameRequestTotal),
				slog.String("error", err.Error()),
			)
		}

		return nil
	}

	return requestMetrics
}

func grpcOutcome(statusCode grpccodes.Code) string {
	if statusCode == grpccodes.OK {
		return "success"
	}

	return "error"
}

func grpcStatus(statusCode grpccodes.Code) string {
	switch statusCode {
	case grpccodes.OK:
		return "ok"
	case grpccodes.Canceled:
		return "cancelled"
	case grpccodes.Unknown:
		return "unknown"
	case grpccodes.InvalidArgument:
		return "invalid_argument"
	case grpccodes.DeadlineExceeded:
		return "deadline_exceeded"
	case grpccodes.NotFound:
		return "not_found"
	case grpccodes.AlreadyExists:
		return "already_exists"
	case grpccodes.PermissionDenied:
		return "permission_denied"
	case grpccodes.ResourceExhausted:
		return "resource_exhausted"
	case grpccodes.FailedPrecondition:
		return "failed_precondition"
	case grpccodes.Aborted:
		return "aborted"
	case grpccodes.OutOfRange:
		return "out_of_range"
	case grpccodes.Unimplemented:
		return "unimplemented"
	case grpccodes.Internal:
		return "internal"
	case grpccodes.Unavailable:
		return "unavailable"
	case grpccodes.DataLoss:
		return "data_loss"
	case grpccodes.Unauthenticated:
		return "unauthenticated"
	default:
		return observability.UnknownMetricAttrValue
	}
}

func grpcMetricStatus(statusCode grpccodes.Code, err error, ctx context.Context, recovered any) string {
	if recovered != nil {
		return "network_error"
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}

	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}

	return grpcStatus(statusCode)
}

func grpcMetricOutcome(statusCode grpccodes.Code, err error, ctx context.Context, recovered any) string {
	if recovered != nil {
		return "error"
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "error"
	}

	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "error"
	}

	return grpcOutcome(statusCode)
}

func (p *InterceptorsProvider) UnaryRecovery() grpcpkg.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpcpkg.UnaryServerInfo, handler grpcpkg.UnaryHandler) (resp any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID := transport.RequestIDFromContext(ctx)
				traceID := logging.TraceIDFromContext(ctx)
				p.logger.Error("panic recovered", logging.PanicFields(
					p.serviceName,
					requestID,
					traceID,
					rec,
					string(debug.Stack()),
				)...)
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

func grpcOperationLabel(fullMethod string) string {
	service, method := parseRPCMethod(fullMethod)
	if service == "" || method == "" {
		return strings.ToLower(strings.TrimSpace(fullMethod))
	}

	return strings.ToLower(strings.TrimSpace(service + "/" + method))
}

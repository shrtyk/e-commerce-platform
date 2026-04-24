package logging

import (
	"context"
	"log/slog"
	
	"go.opentelemetry.io/otel/trace"
)

const (
	FieldService    = "service"
	FieldRequestID  = "request_id"
	FieldTraceID    = "trace_id"
	FieldMethod     = "method"
	FieldPath       = "path"
	FieldStatus     = "status"
	FieldDurationMS = "duration_ms"
	FieldGRPCStatus = "grpc_status"
	FieldPanic      = "panic"
	FieldStack      = "stack"
)

const EmptyTraceID = ""

func RequestFields(service, requestID, traceID, method, path string, status int, durationMS int64) []any {
	return []any{
		slog.String(FieldService, service),
		slog.String(FieldMethod, method),
		slog.String(FieldPath, path),
		slog.Int(FieldStatus, status),
		slog.Int64(FieldDurationMS, durationMS),
		slog.String(FieldRequestID, requestID),
		slog.String(FieldTraceID, traceID),
	}
}

func PanicFields(service, requestID, traceID string, panicValue any, stack string) []any {
	return []any{
		slog.String(FieldService, service),
		slog.String(FieldRequestID, requestID),
		slog.String(FieldTraceID, traceID),
		slog.Any(FieldPanic, panicValue),
		slog.String(FieldStack, stack),
	}
}

func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return EmptyTraceID
	}

	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return EmptyTraceID
	}

	return spanContext.TraceID().String()
}

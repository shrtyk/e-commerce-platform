package observability

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

func TestHTTPTracePropagationRoundtrip(t *testing.T) {
	InitPropagator()

	sourceCtx := traceContextWithBaggage(t)
	sourceSpanContext := oteltrace.SpanContextFromContext(sourceCtx)

	headers := InjectHTTPHeaders(sourceCtx, nil)
	require.NotEmpty(t, headers.Get(TraceParentKey))
	require.NotEmpty(t, headers.Get(TraceStateKey))
	require.NotEmpty(t, headers.Get(BaggageKey))

	extractedCtx := ExtractHTTPHeaders(context.Background(), headers)
	extractedSpanContext := oteltrace.SpanContextFromContext(extractedCtx)

	require.True(t, extractedSpanContext.IsValid())
	require.Equal(t, sourceSpanContext.TraceID(), extractedSpanContext.TraceID())
	require.Equal(t, sourceSpanContext.SpanID(), extractedSpanContext.SpanID())
	require.Equal(t, sourceSpanContext.TraceState().String(), extractedSpanContext.TraceState().String())
	require.Equal(t, "acme", baggage.FromContext(extractedCtx).Member("tenant").Value())
}

func TestHTTPTracePropagationEmptyInput(t *testing.T) {
	InitPropagator()

	injectedHeaders := InjectHTTPHeaders(context.Background(), nil)
	require.NotNil(t, injectedHeaders)

	extractedCtx := ExtractHTTPHeaders(context.Background(), http.Header{})
	require.False(t, oteltrace.SpanContextFromContext(extractedCtx).IsValid())
	require.Empty(t, baggage.FromContext(extractedCtx).Members())

	nilExtractedCtx := ExtractHTTPHeaders(context.Background(), nil)
	require.False(t, oteltrace.SpanContextFromContext(nilExtractedCtx).IsValid())
	require.Empty(t, baggage.FromContext(nilExtractedCtx).Members())
}

func TestGRPCTracePropagationRoundtrip(t *testing.T) {
	InitPropagator()

	sourceCtx := traceContextWithBaggage(t)
	sourceSpanContext := oteltrace.SpanContextFromContext(sourceCtx)

	md := InjectGRPCMetadata(sourceCtx, nil)
	require.NotEmpty(t, md.Get(TraceParentKey))
	require.NotEmpty(t, md.Get(TraceStateKey))
	require.NotEmpty(t, md.Get(BaggageKey))

	extractedCtx := ExtractGRPCMetadata(context.Background(), md)
	extractedSpanContext := oteltrace.SpanContextFromContext(extractedCtx)

	require.True(t, extractedSpanContext.IsValid())
	require.Equal(t, sourceSpanContext.TraceID(), extractedSpanContext.TraceID())
	require.Equal(t, sourceSpanContext.SpanID(), extractedSpanContext.SpanID())
	require.Equal(t, sourceSpanContext.TraceState().String(), extractedSpanContext.TraceState().String())
	require.Equal(t, "acme", baggage.FromContext(extractedCtx).Member("tenant").Value())
}

func TestGRPCTracePropagationEmptyInput(t *testing.T) {
	InitPropagator()

	injectedMetadata := InjectGRPCMetadata(context.Background(), nil)
	require.NotNil(t, injectedMetadata)

	extractedCtx := ExtractGRPCMetadata(context.Background(), metadata.MD{})
	require.False(t, oteltrace.SpanContextFromContext(extractedCtx).IsValid())
	require.Empty(t, baggage.FromContext(extractedCtx).Members())

	nilExtractedCtx := ExtractGRPCMetadata(context.Background(), nil)
	require.False(t, oteltrace.SpanContextFromContext(nilExtractedCtx).IsValid())
	require.Empty(t, baggage.FromContext(nilExtractedCtx).Members())
}

func traceContextWithBaggage(t *testing.T) context.Context {
	t.Helper()

	ctx := context.Background()

	tenantMember, err := baggage.NewMember("tenant", "acme")
	require.NoError(t, err)
	b, err := baggage.New(tenantMember)
	require.NoError(t, err)
	ctx = baggage.ContextWithBaggage(ctx, b)

	spanContext := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		SpanID:     [8]byte{2, 2, 2, 2, 2, 2, 2, 2},
		TraceFlags: oteltrace.FlagsSampled,
		TraceState: mustParseTraceState(t, "acme=state"),
	})

	return oteltrace.ContextWithSpanContext(ctx, spanContext)
}

func mustParseTraceState(t *testing.T, value string) oteltrace.TraceState {
	t.Helper()

	traceState, err := oteltrace.ParseTraceState(value)
	require.NoError(t, err)

	return traceState
}

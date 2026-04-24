package observability

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

const (
	TraceParentKey = "traceparent"
	TraceStateKey  = "tracestate"
	BaggageKey     = "baggage"
)

func InjectHTTPHeaders(ctx context.Context, headers http.Header) http.Header {
	if headers == nil {
		headers = make(http.Header)
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(headers))

	return headers
}

func ExtractHTTPHeaders(ctx context.Context, headers http.Header) context.Context {
	if headers == nil {
		headers = http.Header{}
	}

	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
}

func InjectGRPCMetadata(ctx context.Context, md metadata.MD) metadata.MD {
	if md == nil {
		md = metadata.MD{}
	}

	otel.GetTextMapPropagator().Inject(ctx, grpcMetadataCarrier(md))

	return md
}

func ExtractGRPCMetadata(ctx context.Context, md metadata.MD) context.Context {
	if md == nil {
		md = metadata.MD{}
	}

	return otel.GetTextMapPropagator().Extract(ctx, grpcMetadataCarrier(md))
}

type grpcMetadataCarrier metadata.MD

func (c grpcMetadataCarrier) Get(key string) string {
	values := metadata.MD(c).Get(key)
	if len(values) == 0 {
		return ""
	}

	return values[0]
}

func (c grpcMetadataCarrier) Set(key string, value string) {
	metadata.MD(c).Set(key, value)
}

func (c grpcMetadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}

	return keys
}

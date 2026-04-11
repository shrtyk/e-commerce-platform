package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/shrtyk/e-commerce-platform/internal/common/config"
)

func TestMustCreateTracerProvider(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.OTel
		serviceName string
	}{
		{
			name: "valid config",
			cfg: config.OTel{
				Endpoint: "127.0.0.1:4317",
				Insecure: true,
			},
			serviceName: "cart-svc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var provider any

			require.NotPanics(t, func() {
				provider = MustCreateTracerProvider(tt.cfg, tt.serviceName)
			})

			tp, ok := provider.(*sdktrace.TracerProvider)
			require.True(t, ok)
			require.NotNil(t, tp)
		})
	}
}

func TestMustCreateMeterProvider(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.OTel
		serviceName string
	}{
		{
			name: "valid config",
			cfg: config.OTel{
				Endpoint: "127.0.0.1:4317",
				Insecure: true,
			},
			serviceName: "cart-svc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var provider any

			require.NotPanics(t, func() {
				provider = MustCreateMeterProvider(tt.cfg, tt.serviceName)
			})

			mp, ok := provider.(*sdkmetric.MeterProvider)
			require.True(t, ok)
			require.NotNil(t, mp)
		})
	}
}

func TestShutdown(t *testing.T) {
	tests := []struct {
		name   string
		tracer *sdktrace.TracerProvider
		meter  *sdkmetric.MeterProvider
	}{
		{
			name:  "nil providers",
			meter: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				err := Shutdown(context.Background(), tt.tracer, tt.meter)
				require.NoError(t, err)
			})
		})
	}
}

func TestInitPropagator(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "sets global propagator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				InitPropagator()
			})

			ctx := context.Background()

			bag, err := baggage.NewMember("tenant", "acme")
			require.NoError(t, err)
			bagValues, err := baggage.New(bag)
			require.NoError(t, err)
			ctx = baggage.ContextWithBaggage(ctx, bagValues)

			tctx := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
				TraceID:    [16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
				SpanID:     [8]byte{2, 2, 2, 2, 2, 2, 2, 2},
				TraceFlags: oteltrace.FlagsSampled,
			})
			ctx = oteltrace.ContextWithSpanContext(ctx, tctx)

			carrier := propagation.MapCarrier{}
			otel.GetTextMapPropagator().Inject(ctx, carrier)

			require.NotEmpty(t, carrier.Get("traceparent"))
			require.NotEmpty(t, carrier.Get("baggage"))
			require.Contains(t, carrier.Get("baggage"), "tenant=acme")
		})
	}
}

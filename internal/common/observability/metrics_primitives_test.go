package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewRequestAndBusinessMetrics(t *testing.T) {
	_, _, meter := newTestMeterProvider(t)

	var requestMetrics any
	var businessMetrics any

	require.NotPanics(t, func() {
		var err error
		requestMetrics, err = NewRequestMetrics(meter)
		require.NoError(t, err)

		businessMetrics, err = NewBusinessMetrics(meter)
		require.NoError(t, err)
	})

	require.NotNil(t, requestMetrics)
	require.NotNil(t, businessMetrics)
}

func TestRecordHelpersUseNormalizedLowCardinalityAttributes(t *testing.T) {
	reader, _, meter := newTestMeterProvider(t)

	requestMetrics, err := NewRequestMetrics(meter)
	require.NoError(t, err)

	businessMetrics, err := NewBusinessMetrics(meter)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		requestMetrics.Record(context.Background(), 150*time.Millisecond, RequestMetricAttrs{
			Transport: "HTTP",
			Operation: "/v1/orders/550e8400-e29b-41d4-a716-446655440000/items/123",
			Status:    "200",
			Outcome:   "SUCCESS",
		})

		businessMetrics.RecordEvent(context.Background(), BusinessMetricAttrs{
			Domain:    "Order",
			Operation: "checkout/start",
			Outcome:   "ok",
		})
	})

	rm := collectMetrics(t, reader)

	requestTotal := findMetric(t, rm, MetricNameRequestTotal)
	requestDuration := findMetric(t, rm, MetricNameRequestDurationSeconds)
	businessTotal := findMetric(t, rm, MetricNameBusinessEventTotal)

	requestTotalData, ok := requestTotal.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, requestTotalData.DataPoints, 1)
	require.Equal(t, int64(1), requestTotalData.DataPoints[0].Value)
	assertAttributes(t, requestTotalData.DataPoints[0].Attributes, map[string]string{
		MetricAttrTransport: "http",
		MetricAttrOperation: "/v1/orders/{id}/items/{id}",
		MetricAttrStatus:    "2xx",
		MetricAttrOutcome:   "success",
	})

	requestDurationData, ok := requestDuration.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, requestDurationData.DataPoints, 1)
	require.Equal(t, uint64(1), requestDurationData.DataPoints[0].Count)
	require.Equal(t, 0.15, requestDurationData.DataPoints[0].Sum)
	assertAttributes(t, requestDurationData.DataPoints[0].Attributes, map[string]string{
		MetricAttrTransport: "http",
		MetricAttrOperation: "/v1/orders/{id}/items/{id}",
		MetricAttrStatus:    "2xx",
		MetricAttrOutcome:   "success",
	})

	businessTotalData, ok := businessTotal.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, businessTotalData.DataPoints, 1)
	require.Equal(t, int64(1), businessTotalData.DataPoints[0].Value)
	assertAttributes(t, businessTotalData.DataPoints[0].Attributes, map[string]string{
		MetricAttrDomain:    "order",
		MetricAttrOperation: "checkout/start",
		MetricAttrOutcome:   "success",
	})
}

func TestRecordHelpersUseDeterministicUnknownValuesForEmptyInput(t *testing.T) {
	reader, _, meter := newTestMeterProvider(t)

	requestMetrics, err := NewRequestMetrics(meter)
	require.NoError(t, err)

	businessMetrics, err := NewBusinessMetrics(meter)
	require.NoError(t, err)

	requestMetrics.Record(context.Background(), 0, RequestMetricAttrs{})
	businessMetrics.RecordEvent(context.Background(), BusinessMetricAttrs{})

	rm := collectMetrics(t, reader)

	requestTotal := findMetric(t, rm, MetricNameRequestTotal)
	requestTotalData, ok := requestTotal.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, requestTotalData.DataPoints, 1)
	assertAttributes(t, requestTotalData.DataPoints[0].Attributes, map[string]string{
		MetricAttrTransport: UnknownMetricAttrValue,
		MetricAttrOperation: UnknownMetricAttrValue,
		MetricAttrStatus:    UnknownMetricAttrValue,
		MetricAttrOutcome:   UnknownMetricAttrValue,
	})

	businessTotal := findMetric(t, rm, MetricNameBusinessEventTotal)
	businessTotalData, ok := businessTotal.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, businessTotalData.DataPoints, 1)
	assertAttributes(t, businessTotalData.DataPoints[0].Attributes, map[string]string{
		MetricAttrDomain:    UnknownMetricAttrValue,
		MetricAttrOperation: UnknownMetricAttrValue,
		MetricAttrOutcome:   UnknownMetricAttrValue,
	})
}

func TestNormalizeHelpers(t *testing.T) {
	t.Cleanup(ResetDomainAllowlist)

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "unknown transport", got: NormalizeTransport("kafka"), want: UnknownMetricAttrValue},
		{name: "http status class", got: NormalizeStatus("503"), want: "5xx"},
		{name: "timeout status", got: NormalizeStatus("timeout"), want: "timeout"},
		{name: "network error status", got: NormalizeStatus("network_error"), want: "network_error"},
		{name: "grpc status", got: NormalizeStatus("Unavailable"), want: "unavailable"},
		{name: "invalid status unknown", got: NormalizeStatus("9999"), want: UnknownMetricAttrValue},
		{name: "empty operation", got: NormalizeOperation("   "), want: UnknownMetricAttrValue},
		{name: "operation id masking", got: NormalizeOperation("/users/123/orders/550e8400-e29b-41d4-a716-446655440000"), want: "/users/{id}/orders/{id}"},
		{name: "operation preserve route name", got: NormalizeOperation("/products/search"), want: "/products/search"},
		{name: "operation preserve human readable slug", got: NormalizeOperation("/catalog/summer-sale"), want: "/catalog/summer-sale"},
		{name: "operation preserve human readable slug with digits product", got: NormalizeOperation("/products/iphone-15-pro"), want: "/products/iphone-15-pro"},
		{name: "operation preserve human readable slug with digits ranking", got: NormalizeOperation("/blog/top-10-products"), want: "/blog/top-10-products"},
		{name: "operation query only unknown", got: NormalizeOperation("?request_id=123"), want: UnknownMetricAttrValue},
		{name: "operation invalid symbols unknown", got: NormalizeOperation("/orders/$%/items"), want: UnknownMetricAttrValue},
		{name: "operation mixed alphanumeric id masking", got: NormalizeOperation("/orders/abc123def456/items"), want: "/orders/{id}/items"},
		{name: "operation pure alpha slug preserved", got: NormalizeOperation("/users/johndoe/orders"), want: "/users/johndoe/orders"},
		{name: "operation canonical separators", got: NormalizeOperation("//users///123/orders//"), want: "/users/{id}/orders"},
		{name: "operation root canonical separators", got: NormalizeOperation("///"), want: "/"},
		{name: "unknown outcome", got: NormalizeOutcome("pending"), want: UnknownMetricAttrValue},
		{name: "ok outcome", got: NormalizeOutcome("ok"), want: "success"},
		{name: "unknown domain non-empty", got: NormalizeDomain("payments-v2"), want: UnknownMetricAttrValue},
		{name: "known domain", got: NormalizeDomain("Order"), want: "order"},
		{name: "domain symbols unknown", got: NormalizeDomain("@@@"), want: UnknownMetricAttrValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}

func TestSetDomainAllowlist(t *testing.T) {
	t.Cleanup(ResetDomainAllowlist)

	ResetDomainAllowlist()
	SetDomainAllowlist([]string{"order-events", "fulfillment center"})

	require.Equal(t, "order_events", NormalizeDomain("order-events"))
	require.Equal(t, "fulfillment_center", NormalizeDomain("fulfillment center"))
	require.Equal(t, UnknownMetricAttrValue, NormalizeDomain("order"))
	require.Equal(t, UnknownMetricAttrValue, NormalizeDomain("payment"))

	ResetDomainAllowlist()
	require.Equal(t, "order", NormalizeDomain("order"))
}

func TestSetDomainAllowlistRejectsUnsafeLabels(t *testing.T) {
	t.Cleanup(ResetDomainAllowlist)

	ResetDomainAllowlist()
	SetDomainAllowlist([]string{"good-domain", "bad@domain", "full stop.domain", "UPPER CASE"})

	require.Equal(t, "good_domain", NormalizeDomain("good-domain"))
	require.Equal(t, "upper_case", NormalizeDomain("upper case"))
	require.Equal(t, UnknownMetricAttrValue, NormalizeDomain("bad@domain"))
	require.Equal(t, UnknownMetricAttrValue, NormalizeDomain("full stop.domain"))
}

func TestRecordClampsNegativeDurationToZero(t *testing.T) {
	reader, _, meter := newTestMeterProvider(t)

	requestMetrics, err := NewRequestMetrics(meter)
	require.NoError(t, err)

	requestMetrics.Record(context.Background(), -1*time.Second, RequestMetricAttrs{
		Transport: "http",
		Operation: "/orders/123",
		Status:    "200",
		Outcome:   "success",
	})

	rm := collectMetrics(t, reader)
	requestDuration := findMetric(t, rm, MetricNameRequestDurationSeconds)

	requestDurationData, ok := requestDuration.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, requestDurationData.DataPoints, 1)
	require.Equal(t, uint64(1), requestDurationData.DataPoints[0].Count)
	require.Equal(t, float64(0), requestDurationData.DataPoints[0].Sum)
}

func newTestMeterProvider(t *testing.T) (*sdkmetric.ManualReader, *sdkmetric.MeterProvider, metric.Meter) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(newResource("metrics-primitives-test")),
	)

	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	return reader, provider, provider.Meter("internal/common/observability/test")
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	return rm
}

func findMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metricValue := range scope.Metrics {
			if metricValue.Name == name {
				return metricValue
			}
		}
	}

	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func assertAttributes(t *testing.T, attrs attribute.Set, expected map[string]string) {
	t.Helper()

	require.Equal(t, len(expected), attrs.Len())

	for key, expectedValue := range expected {
		value, ok := attrs.Value(attribute.Key(key))
		require.True(t, ok, "missing attribute %q", key)
		require.Equal(t, expectedValue, value.AsString())
	}
}

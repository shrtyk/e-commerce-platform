package observability

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	MetricNameRequestTotal           = "platform.request.total"
	MetricNameRequestDurationSeconds = "platform.request.duration.seconds"
	MetricNameBusinessEventTotal     = "platform.business.event.total"
)

const (
	MetricAttrTransport = "transport"
	MetricAttrOperation = "operation"
	MetricAttrStatus    = "status"
	MetricAttrOutcome   = "outcome"
	MetricAttrDomain    = "domain"
)

const UnknownMetricAttrValue = "unknown"

var (
	uuidSegmentPattern              = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hexSegmentPattern               = regexp.MustCompile(`^[0-9a-f]{12,}$`)
	numericSegmentPattern           = regexp.MustCompile(`^[0-9]+$`)
	operationSegmentPattern         = regexp.MustCompile(`^[a-z0-9._{}-]+$`)
	opaqueSegmentPattern            = regexp.MustCompile(`^[a-z0-9]{16,}$`)
	domainLabelPattern             = regexp.MustCompile(`^[a-z0-9_]+$`)

	defaultAllowedDomainValues = map[string]struct{}{
		"identity":     {},
		"catalog":      {},
		"inventory":    {},
		"cart":         {},
		"order":        {},
		"payment":      {},
		"checkout":     {},
		"notification": {},
		"shipping":     {},
	}

	allowedDomainValues   = cloneStringSet(defaultAllowedDomainValues)
	allowedDomainValuesMu sync.RWMutex

	grpcStatusValues = map[string]struct{}{
		"ok":                  {},
		"cancelled":           {},
		"unknown":             {},
		"invalid_argument":    {},
		"deadline_exceeded":   {},
		"not_found":           {},
		"already_exists":      {},
		"permission_denied":   {},
		"resource_exhausted":  {},
		"failed_precondition": {},
		"aborted":             {},
		"out_of_range":        {},
		"unimplemented":       {},
		"internal":            {},
		"unavailable":         {},
		"data_loss":           {},
		"unauthenticated":     {},
		"timeout":             {},
		"network_error":       {},
	}
)

type RequestMetricAttrs struct {
	Transport string
	Operation string
	Status    string
	Outcome   string
}

type BusinessMetricAttrs struct {
	Domain    string
	Operation string
	Outcome   string
}

type RequestMetrics struct {
	requestTotal    metric.Int64Counter
	requestDuration metric.Float64Histogram
}

func NewRequestMetrics(meter metric.Meter) (*RequestMetrics, error) {
	requestTotal, err := meter.Int64Counter(
		MetricNameRequestTotal,
		metric.WithDescription("Total number of processed transport requests."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	requestDuration, err := meter.Float64Histogram(
		MetricNameRequestDurationSeconds,
		metric.WithDescription("Duration of processed transport requests in seconds."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	return &RequestMetrics{
		requestTotal:    requestTotal,
		requestDuration: requestDuration,
	}, nil
}

func (m *RequestMetrics) Record(ctx context.Context, duration time.Duration, attrs RequestMetricAttrs) {
	if m == nil {
		return
	}

	if duration < 0 {
		duration = 0
	}

	options := metric.WithAttributeSet(attribute.NewSet(
		attribute.String(MetricAttrTransport, NormalizeTransport(attrs.Transport)),
		attribute.String(MetricAttrOperation, NormalizeOperation(attrs.Operation)),
		attribute.String(MetricAttrStatus, NormalizeStatus(attrs.Status)),
		attribute.String(MetricAttrOutcome, NormalizeOutcome(attrs.Outcome)),
	))

	m.requestTotal.Add(ensureContext(ctx), 1, options)
	m.requestDuration.Record(ensureContext(ctx), duration.Seconds(), options)
}

type BusinessMetrics struct {
	eventsTotal metric.Int64Counter
}

func NewBusinessMetrics(meter metric.Meter) (*BusinessMetrics, error) {
	eventsTotal, err := meter.Int64Counter(
		MetricNameBusinessEventTotal,
		metric.WithDescription("Total number of business-domain events emitted by service code paths."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return nil, err
	}

	return &BusinessMetrics{eventsTotal: eventsTotal}, nil
}

func (m *BusinessMetrics) RecordEvent(ctx context.Context, attrs BusinessMetricAttrs) {
	if m == nil {
		return
	}

	options := metric.WithAttributeSet(attribute.NewSet(
		attribute.String(MetricAttrDomain, NormalizeDomain(attrs.Domain)),
		attribute.String(MetricAttrOperation, NormalizeOperation(attrs.Operation)),
		attribute.String(MetricAttrOutcome, NormalizeOutcome(attrs.Outcome)),
	))

	m.eventsTotal.Add(ensureContext(ctx), 1, options)
}

func NormalizeTransport(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http":
		return "http"
	case "grpc":
		return "grpc"
	default:
		return UnknownMetricAttrValue
	}
}

func NormalizeStatus(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized == "" {
		return UnknownMetricAttrValue
	}

	if len(normalized) == 3 {
		if code, err := strconv.Atoi(normalized); err == nil && code >= 100 && code <= 599 {
			return normalized[:1] + "xx"
		}
	}

	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.ReplaceAll(normalized, "-", "_")

	if _, ok := grpcStatusValues[normalized]; ok {
		return normalized
	}

	return UnknownMetricAttrValue
}

func NormalizeOutcome(outcome string) string {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "success", "succeeded", "ok", "true":
		return "success"
	case "error", "fail", "failed", "failure", "false":
		return "error"
	default:
		return UnknownMetricAttrValue
	}
}

func NormalizeDomain(domain string) string {
	normalized := strings.ToLower(strings.TrimSpace(domain))
	if normalized == "" {
		return UnknownMetricAttrValue
	}

	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.ReplaceAll(normalized, "-", "_")

	if len(normalized) > 40 {
		return UnknownMetricAttrValue
	}

	allowedDomainValuesMu.RLock()
	defer allowedDomainValuesMu.RUnlock()

	if _, ok := allowedDomainValues[normalized]; ok {
		return normalized
	}

	return UnknownMetricAttrValue
}

func NormalizeOperation(operation string) string {
	normalized := strings.ToLower(strings.TrimSpace(operation))
	if normalized == "" {
		return UnknownMetricAttrValue
	}

	isPathLike := strings.HasPrefix(normalized, "/")

	if queryIndex := strings.Index(normalized, "?"); queryIndex >= 0 {
		normalized = normalized[:queryIndex]
	}

	if normalized == "" {
		return UnknownMetricAttrValue
	}

	rawParts := strings.Split(normalized, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if strings.TrimSpace(part) == "" {
			continue
		}

		normalizedPart, ok := normalizeOperationSegment(part)
		if !ok {
			return UnknownMetricAttrValue
		}

		parts = append(parts, normalizedPart)
	}

	if len(parts) == 0 {
		if isPathLike {
			return "/"
		}

		return UnknownMetricAttrValue
	}

	normalized = strings.Join(parts, "/")
	if isPathLike {
		normalized = "/" + normalized
	}

	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return UnknownMetricAttrValue
	}

	if len(normalized) > 120 {
		return UnknownMetricAttrValue
	}

	return normalized
}

func normalizeOperationSegment(segment string) (string, bool) {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return "", true
	}

	if numericSegmentPattern.MatchString(segment) {
		return "{id}", true
	}

	if uuidSegmentPattern.MatchString(segment) {
		return "{id}", true
	}

	if hexSegmentPattern.MatchString(segment) || (opaqueSegmentPattern.MatchString(segment) && hasLetterAndDigit(segment)) {
		return "{id}", true
	}

	if len(segment) > 48 {
		return "{id}", true
	}

	if !operationSegmentPattern.MatchString(segment) {
		return "", false
	}

	return segment, true
}

func hasLetterAndDigit(value string) bool {
	hasLetter := false
	hasDigit := false

	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' {
			hasLetter = true
		}

		if ch >= '0' && ch <= '9' {
			hasDigit = true
		}

		if hasLetter && hasDigit {
			return true
		}
	}

	return false
}

func SetDomainAllowlist(domains []string) {
	allowedDomainValuesMu.Lock()
	defer allowedDomainValuesMu.Unlock()

	next := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if normalized == "" {
			continue
		}

		normalized = strings.ReplaceAll(normalized, " ", "_")
		normalized = strings.ReplaceAll(normalized, "-", "_")
		if len(normalized) > 40 {
			continue
		}

		if !domainLabelPattern.MatchString(normalized) {
			continue
		}

		next[normalized] = struct{}{}
	}

	allowedDomainValues = next
}

func ResetDomainAllowlist() {
	allowedDomainValuesMu.Lock()
	defer allowedDomainValuesMu.Unlock()

	allowedDomainValues = cloneStringSet(defaultAllowedDomainValues)
}

func cloneStringSet(source map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(source))
	for key := range source {
		cloned[key] = struct{}{}
	}

	return cloned
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}

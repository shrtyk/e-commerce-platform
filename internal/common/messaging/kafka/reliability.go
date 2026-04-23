package kafka

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderRetryAttempt       = "x-retry-attempt"
	HeaderRetryMaxAttempts   = "x-retry-max-attempts"
	HeaderRetryOriginalTopic = "x-retry-original-topic"
	HeaderRetryFirstFailedAt = "x-retry-first-failed-at"
	HeaderRetryLastFailedAt  = "x-retry-last-failed-at"
	HeaderRetryErrorCode     = "x-retry-error-code"
	HeaderRetryErrorMessage  = "x-retry-error-message"
	HeaderRetryConsumerGroup = "x-retry-consumer-group"
	HeaderDLQReason          = "x-dlq-reason"
	HeaderDLQAt              = "x-dlq-at"
)

const (
	DLQReasonMaxAttemptsExceeded = "max-attempts-exceeded"
	DLQReasonNonRetryable        = "non-retryable"
)

type RoutingTarget string

const (
	RoutingTargetNone  RoutingTarget = "none"
	RoutingTargetRetry RoutingTarget = "retry"
	RoutingTargetDLQ   RoutingTarget = "dlq"
)

type FailureClassification string

const (
	FailureNone         FailureClassification = "none"
	FailureRetriable    FailureClassification = "retriable"
	FailureNonRetriable FailureClassification = "non-retriable"
)

type RetryMetadata struct {
	Attempt       int
	MaxAttempts   int
	OriginalTopic string
	FirstFailedAt time.Time
}

type RoutingDecision struct {
	Target   RoutingTarget
	Envelope EventEnvelope
}

type ReliabilityRouter struct {
	consumerGroup string
	maxAttempts   int
	now           func() time.Time
}

func NewReliabilityRouter(consumerGroup string, maxAttempts int) (*ReliabilityRouter, error) {
	if consumerGroup == "" {
		return nil, fmt.Errorf("consumer group is empty")
	}

	if maxAttempts < 1 {
		return nil, fmt.Errorf("max attempts must be >= 1")
	}

	return &ReliabilityRouter{
		consumerGroup: consumerGroup,
		maxAttempts:   maxAttempts,
		now:           time.Now,
	}, nil
}

func ParseRetryMetadata(headers map[string]string, sourceTopic string, defaultMaxAttempts int) RetryMetadata {
	metadata := RetryMetadata{
		Attempt:       parsePositiveInt(headers[HeaderRetryAttempt], 0),
		MaxAttempts:   parseStrictlyPositiveInt(headers[HeaderRetryMaxAttempts], defaultMaxAttempts),
		OriginalTopic: headers[HeaderRetryOriginalTopic],
	}

	if metadata.MaxAttempts < 1 {
		metadata.MaxAttempts = 1
	}

	if metadata.OriginalTopic == "" {
		metadata.OriginalTopic = inferOriginalTopic(sourceTopic)
	}

	if firstFailedAt, err := time.Parse(time.RFC3339, headers[HeaderRetryFirstFailedAt]); err == nil {
		metadata.FirstFailedAt = firstFailedAt.UTC()
	}

	return metadata
}

func (r *ReliabilityRouter) RouteFailure(envelope EventEnvelope, classification FailureClassification, errorCode, errorMessage string) (RoutingDecision, error) {
	switch classification {
	case FailureNone:
		return RoutingDecision{Target: RoutingTargetNone}, nil
	case FailureRetriable, FailureNonRetriable:
		// continue
	default:
		return RoutingDecision{}, fmt.Errorf("unknown failure classification: %q", classification)
	}

	if envelope.Topic == "" {
		return RoutingDecision{}, fmt.Errorf("source topic is empty")
	}

	now := r.now().UTC()
	metadata := ParseRetryMetadata(envelope.Headers, envelope.Topic, r.maxAttempts)
	if metadata.FirstFailedAt.IsZero() {
		metadata.FirstFailedAt = now
	}

	headers := make(map[string]string, len(envelope.Headers)+10)
	maps.Copy(headers, envelope.Headers)

	headers[HeaderRetryMaxAttempts] = strconv.Itoa(metadata.MaxAttempts)
	headers[HeaderRetryOriginalTopic] = metadata.OriginalTopic
	headers[HeaderRetryFirstFailedAt] = metadata.FirstFailedAt.Format(time.RFC3339)
	headers[HeaderRetryLastFailedAt] = now.Format(time.RFC3339)
	headers[HeaderRetryErrorCode] = errorCode
	headers[HeaderRetryErrorMessage] = errorMessage
	headers[HeaderRetryConsumerGroup] = r.consumerGroup

	decision := RoutingDecision{
		Envelope: EventEnvelope{
			Key:      envelope.Key,
			Payload:  envelope.Payload,
			Headers:  headers,
			Metadata: envelope.Metadata,
		},
	}

	if classification == FailureNonRetriable {
		headers[HeaderRetryAttempt] = strconv.Itoa(metadata.Attempt)
		headers[HeaderDLQReason] = DLQReasonNonRetryable
		headers[HeaderDLQAt] = now.Format(time.RFC3339)
		decision.Target = RoutingTargetDLQ
		decision.Envelope.Topic = metadata.OriginalTopic + ".dlq"
		return decision, nil
	}

	if metadata.Attempt >= metadata.MaxAttempts {
		headers[HeaderRetryAttempt] = strconv.Itoa(metadata.Attempt)
		headers[HeaderDLQReason] = DLQReasonMaxAttemptsExceeded
		headers[HeaderDLQAt] = now.Format(time.RFC3339)
		decision.Target = RoutingTargetDLQ
		decision.Envelope.Topic = metadata.OriginalTopic + ".dlq"
		return decision, nil
	}

	headers[HeaderRetryAttempt] = strconv.Itoa(metadata.Attempt + 1)
	delete(headers, HeaderDLQReason)
	delete(headers, HeaderDLQAt)

	decision.Target = RoutingTargetRetry
	decision.Envelope.Topic = metadata.OriginalTopic + ".retry"

	return decision, nil
}

func parsePositiveInt(raw string, fallback int) int {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return fallback
	}

	return parsed
}

func parseStrictlyPositiveInt(raw string, fallback int) int {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func inferOriginalTopic(sourceTopic string) string {
	if s, ok := strings.CutSuffix(sourceTopic, ".retry"); ok {
		return s
	}

	if s, ok := strings.CutSuffix(sourceTopic, ".dlq"); ok {
		return s
	}

	return sourceTopic
}

package kafka

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReliabilityRouterRouteFailure(t *testing.T) {
	now := time.Date(2026, 4, 23, 10, 11, 12, 0, time.UTC)
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)
	router.now = func() time.Time { return now }

	baseEnvelope := EventEnvelope{
		Topic:   "payment.events",
		Key:     []byte("order-1"),
		Payload: []byte("payload"),
		Headers: map[string]string{
			HeaderEventID:   "evt-1",
			HeaderEventName: "payment.authorized",
		},
		Metadata: EventMetadata{EventID: "evt-1"},
	}

	tests := []struct {
		name              string
		envelope          EventEnvelope
		classification    FailureClassification
		wantTarget        RoutingTarget
		wantTopic         string
		wantAttempt       string
		wantMaxAttempts   string
		wantOriginalTopic string
		wantDLQReason     string
	}{
		{
			name:              "first failure from main topic to retry",
			envelope:          baseEnvelope,
			classification:    FailureRetriable,
			wantTarget:        RoutingTargetRetry,
			wantTopic:         "payment.events.retry",
			wantAttempt:       "1",
			wantMaxAttempts:   "3",
			wantOriginalTopic: "payment.events",
		},
		{
			name: "retriable failure below max increments attempt",
			envelope: EventEnvelope{
				Topic:   "payment.events.retry",
				Key:     []byte("order-1"),
				Payload: []byte("payload"),
				Headers: map[string]string{
					HeaderRetryAttempt:       "1",
					HeaderRetryMaxAttempts:   "3",
					HeaderRetryOriginalTopic: "payment.events",
				},
			},
			classification:    FailureRetriable,
			wantTarget:        RoutingTargetRetry,
			wantTopic:         "payment.events.retry",
			wantAttempt:       "2",
			wantMaxAttempts:   "3",
			wantOriginalTopic: "payment.events",
		},
		{
			name: "retriable failure at max goes dlq",
			envelope: EventEnvelope{
				Topic:   "payment.events.retry",
				Key:     []byte("order-1"),
				Payload: []byte("payload"),
				Headers: map[string]string{
					HeaderRetryAttempt:       "3",
					HeaderRetryMaxAttempts:   "3",
					HeaderRetryOriginalTopic: "payment.events",
				},
			},
			classification:    FailureRetriable,
			wantTarget:        RoutingTargetDLQ,
			wantTopic:         "payment.events.dlq",
			wantAttempt:       "3",
			wantMaxAttempts:   "3",
			wantOriginalTopic: "payment.events",
			wantDLQReason:     DLQReasonMaxAttemptsExceeded,
		},
		{
			name:              "non-retriable failure goes dlq",
			envelope:          baseEnvelope,
			classification:    FailureNonRetriable,
			wantTarget:        RoutingTargetDLQ,
			wantTopic:         "payment.events.dlq",
			wantAttempt:       "0",
			wantMaxAttempts:   "3",
			wantOriginalTopic: "payment.events",
			wantDLQReason:     DLQReasonNonRetryable,
		},
		{
			name: "original topic preserved when source already retry",
			envelope: EventEnvelope{
				Topic:   "payment.events.retry",
				Key:     []byte("order-1"),
				Payload: []byte("payload"),
				Headers: map[string]string{
					HeaderRetryAttempt:       "1",
					HeaderRetryMaxAttempts:   "3",
					HeaderRetryOriginalTopic: "payment.events",
				},
			},
			classification:    FailureRetriable,
			wantTarget:        RoutingTargetRetry,
			wantTopic:         "payment.events.retry",
			wantAttempt:       "2",
			wantMaxAttempts:   "3",
			wantOriginalTopic: "payment.events",
		},
		{
			name: "malformed retry headers handled deterministically",
			envelope: EventEnvelope{
				Topic:   "payment.events.retry",
				Key:     []byte("order-1"),
				Payload: []byte("payload"),
				Headers: map[string]string{
					HeaderRetryAttempt:       "bad",
					HeaderRetryMaxAttempts:   "bad",
					HeaderRetryOriginalTopic: "payment.events",
				},
			},
			classification:    FailureRetriable,
			wantTarget:        RoutingTargetRetry,
			wantTopic:         "payment.events.retry",
			wantAttempt:       "1",
			wantMaxAttempts:   "3",
			wantOriginalTopic: "payment.events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, routeErr := router.RouteFailure(tt.envelope, tt.classification, "HANDLER_FAILED", "handler failed")
			require.NoError(t, routeErr)

			require.Equal(t, tt.wantTarget, decision.Target)
			require.Equal(t, tt.wantTopic, decision.Envelope.Topic)
			require.Equal(t, tt.envelope.Key, decision.Envelope.Key)
			require.Equal(t, tt.envelope.Payload, decision.Envelope.Payload)
			require.Equal(t, tt.wantAttempt, decision.Envelope.Headers[HeaderRetryAttempt])
			require.Equal(t, tt.wantMaxAttempts, decision.Envelope.Headers[HeaderRetryMaxAttempts])
			require.Equal(t, tt.wantOriginalTopic, decision.Envelope.Headers[HeaderRetryOriginalTopic])
			require.Equal(t, now.Format(time.RFC3339), decision.Envelope.Headers[HeaderRetryLastFailedAt])
			require.Equal(t, "HANDLER_FAILED", decision.Envelope.Headers[HeaderRetryErrorCode])
			require.Equal(t, "handler failed", decision.Envelope.Headers[HeaderRetryErrorMessage])
			require.Equal(t, "order-consumer", decision.Envelope.Headers[HeaderRetryConsumerGroup])

			if tt.wantDLQReason == "" {
				require.NotContains(t, decision.Envelope.Headers, HeaderDLQReason)
				require.NotContains(t, decision.Envelope.Headers, HeaderDLQAt)
				return
			}

			require.Equal(t, tt.wantDLQReason, decision.Envelope.Headers[HeaderDLQReason])
			require.Equal(t, now.Format(time.RFC3339), decision.Envelope.Headers[HeaderDLQAt])
		})
	}
}

func TestReliabilityRouterRouteFailureNone(t *testing.T) {
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)

	decision, routeErr := router.RouteFailure(EventEnvelope{Topic: "payment.events"}, FailureNone, "", "")
	require.NoError(t, routeErr)
	require.Equal(t, RoutingTargetNone, decision.Target)
}

func TestReliabilityRouterRouteFailureUnknownClassification(t *testing.T) {
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)

	decision, routeErr := router.RouteFailure(
		EventEnvelope{Topic: "payment.events"},
		FailureClassification("unknown"),
		"HANDLER_FAILED",
		"handler failed",
	)

	require.Error(t, routeErr)
	require.Contains(t, routeErr.Error(), "unknown failure classification")
	require.Equal(t, RoutingTarget(""), decision.Target)
}

func TestReliabilityRouterRouteFailureRetryTopicInfersOriginalWhenMissingHeader(t *testing.T) {
	now := time.Date(2026, 4, 23, 10, 11, 12, 0, time.UTC)
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)
	router.now = func() time.Time { return now }

	decision, routeErr := router.RouteFailure(
		EventEnvelope{
			Topic:   "payment.events.retry",
			Key:     []byte("order-1"),
			Payload: []byte("payload"),
			Headers: map[string]string{},
		},
		FailureRetriable,
		"HANDLER_FAILED",
		"handler failed",
	)
	require.NoError(t, routeErr)
	require.Equal(t, RoutingTargetRetry, decision.Target)
	require.Equal(t, "payment.events.retry", decision.Envelope.Topic)
	require.Equal(t, "payment.events", decision.Envelope.Headers[HeaderRetryOriginalTopic])
}

func TestReliabilityRouterRouteFailureMalformedMaxAttemptsFallsBackToDefault(t *testing.T) {
	now := time.Date(2026, 4, 23, 10, 11, 12, 0, time.UTC)
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)
	router.now = func() time.Time { return now }

	decision, routeErr := router.RouteFailure(
		EventEnvelope{
			Topic:   "payment.events.retry",
			Key:     []byte("order-1"),
			Payload: []byte("payload"),
			Headers: map[string]string{
				HeaderRetryAttempt:       "1",
				HeaderRetryMaxAttempts:   "0",
				HeaderRetryOriginalTopic: "payment.events",
			},
		},
		FailureRetriable,
		"HANDLER_FAILED",
		"handler failed",
	)
	require.NoError(t, routeErr)
	require.Equal(t, RoutingTargetRetry, decision.Target)
	require.Equal(t, "3", decision.Envelope.Headers[HeaderRetryMaxAttempts])
}

func TestReliabilityRouterRouteFailureMalformedFirstFailedAtResetsToNow(t *testing.T) {
	now := time.Date(2026, 4, 23, 10, 11, 12, 0, time.UTC)
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)
	router.now = func() time.Time { return now }

	decision, routeErr := router.RouteFailure(
		EventEnvelope{
			Topic:   "payment.events.retry",
			Key:     []byte("order-1"),
			Payload: []byte("payload"),
			Headers: map[string]string{
				HeaderRetryAttempt:       "1",
				HeaderRetryMaxAttempts:   "3",
				HeaderRetryOriginalTopic: "payment.events",
				HeaderRetryFirstFailedAt: "bad-ts",
			},
		},
		FailureRetriable,
		"HANDLER_FAILED",
		"handler failed",
	)
	require.NoError(t, routeErr)
	require.Equal(t, now.Format(time.RFC3339), decision.Envelope.Headers[HeaderRetryFirstFailedAt])
	require.Equal(t, now.Format(time.RFC3339), decision.Envelope.Headers[HeaderRetryLastFailedAt])
}

func TestReliabilityRouterRouteFailurePreservesFirstFailedAtAndUpdatesLastFailedAt(t *testing.T) {
	now := time.Date(2026, 4, 23, 10, 11, 12, 0, time.UTC)
	firstFailedAt := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)
	router.now = func() time.Time { return now }

	decision, routeErr := router.RouteFailure(
		EventEnvelope{
			Topic:   "payment.events.retry",
			Key:     []byte("order-1"),
			Payload: []byte("payload"),
			Headers: map[string]string{
				HeaderRetryAttempt:       "1",
				HeaderRetryMaxAttempts:   "3",
				HeaderRetryOriginalTopic: "payment.events",
				HeaderRetryFirstFailedAt: firstFailedAt.Format(time.RFC3339),
				HeaderRetryLastFailedAt:  time.Date(2026, 4, 23, 9, 30, 0, 0, time.UTC).Format(time.RFC3339),
			},
		},
		FailureRetriable,
		"HANDLER_FAILED",
		"handler failed",
	)
	require.NoError(t, routeErr)
	require.Equal(t, firstFailedAt.Format(time.RFC3339), decision.Envelope.Headers[HeaderRetryFirstFailedAt])
	require.Equal(t, now.Format(time.RFC3339), decision.Envelope.Headers[HeaderRetryLastFailedAt])
}

func TestReliabilityRouterRouteFailureRetryClearsStaleDLQHeaders(t *testing.T) {
	now := time.Date(2026, 4, 23, 10, 11, 12, 0, time.UTC)
	router, err := NewReliabilityRouter("order-consumer", 3)
	require.NoError(t, err)
	router.now = func() time.Time { return now }

	decision, routeErr := router.RouteFailure(
		EventEnvelope{
			Topic:   "payment.events.retry",
			Key:     []byte("order-1"),
			Payload: []byte("payload"),
			Headers: map[string]string{
				HeaderRetryAttempt:       "1",
				HeaderRetryMaxAttempts:   "3",
				HeaderRetryOriginalTopic: "payment.events",
				HeaderDLQReason:          DLQReasonNonRetryable,
				HeaderDLQAt:              time.Date(2026, 4, 23, 9, 30, 0, 0, time.UTC).Format(time.RFC3339),
			},
		},
		FailureRetriable,
		"HANDLER_FAILED",
		"handler failed",
	)
	require.NoError(t, routeErr)
	require.Equal(t, RoutingTargetRetry, decision.Target)
	require.NotContains(t, decision.Envelope.Headers, HeaderDLQReason)
	require.NotContains(t, decision.Envelope.Headers, HeaderDLQAt)
}

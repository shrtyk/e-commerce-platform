package kafka

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMetadataHeaderMappingRoundTrip(t *testing.T) {
	occurredAt := time.Date(2026, 4, 10, 12, 30, 45, 123000000, time.UTC)
	metadata := EventMetadata{
		EventID:       "evt-1",
		EventName:     "catalog.product.created",
		Producer:      "catalog-svc",
		OccurredAt:    occurredAt,
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		SchemaVersion: "v1",
	}

	headers := map[string]string{"custom": "value"}
	mapped := MetadataToHeaders(metadata, headers)
	require.Equal(t, "value", mapped["custom"])
	require.Equal(t, "evt-1", mapped[HeaderEventID])
	require.Equal(t, "catalog.product.created", mapped[HeaderEventName])
	require.Equal(t, "catalog-svc", mapped[HeaderProducer])
	require.Equal(t, occurredAt.Format(time.RFC3339Nano), mapped[HeaderOccurredAt])
	require.Equal(t, "corr-1", mapped[HeaderCorrelationID])
	require.Equal(t, "cause-1", mapped[HeaderCausationID])
	require.Equal(t, "v1", mapped[HeaderSchemaVersion])

	roundTrip := MetadataFromHeaders(mapped)
	require.Equal(t, metadata, roundTrip)
}

func TestRetryPolicyBackoffForAttempt(t *testing.T) {
	policy := RetryPolicy{
		MaxAttempts: 4,
		Backoff:     100 * time.Millisecond,
		Multiplier:  2,
		MaxBackoff:  250 * time.Millisecond,
	}

	require.Equal(t, time.Duration(0), policy.BackoffForAttempt(1))
	require.Equal(t, 100*time.Millisecond, policy.BackoffForAttempt(2))
	require.Equal(t, 200*time.Millisecond, policy.BackoffForAttempt(3))
	require.Equal(t, 250*time.Millisecond, policy.BackoffForAttempt(4))
}

package kafka

import (
	"fmt"
	"maps"
	"time"

	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	HeaderEventID       = "eventId"
	HeaderEventName     = "eventName"
	HeaderProducer      = "producer"
	HeaderOccurredAt    = "occurredAt"
	HeaderCorrelationID = "correlationId"
	HeaderCausationID   = "causationId"
	HeaderSchemaVersion = "schemaVersion"
	HeaderRecordName    = "recordName"
)

type EventMetadata struct {
	EventID       string
	EventName     string
	Producer      string
	OccurredAt    time.Time
	CorrelationID string
	CausationID   string
	SchemaVersion string
}

type EventEnvelope struct {
	Topic    string
	Key      []byte
	Headers  map[string]string
	Payload  []byte
	Metadata EventMetadata
}

type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
	Multiplier  float64
	MaxBackoff  time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		Backoff:     100 * time.Millisecond,
		Multiplier:  2,
		MaxBackoff:  2 * time.Second,
	}
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 1 {
		return fmt.Errorf("max attempts must be >= 1")
	}

	if p.Backoff < 0 {
		return fmt.Errorf("backoff must be >= 0")
	}

	if p.Multiplier <= 0 {
		return fmt.Errorf("multiplier must be > 0")
	}

	if p.MaxBackoff < 0 {
		return fmt.Errorf("max backoff must be >= 0")
	}

	return nil
}

func (p RetryPolicy) BackoffForAttempt(attempt int) time.Duration {
	if attempt <= 1 || p.Backoff == 0 {
		return 0
	}

	value := float64(p.Backoff)
	for i := 2; i < attempt; i++ {
		value *= p.Multiplier
	}

	calculated := time.Duration(value)
	if p.MaxBackoff > 0 && calculated > p.MaxBackoff {
		return p.MaxBackoff
	}

	return calculated
}

func MetadataToHeaders(metadata EventMetadata, headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers)+7)
	maps.Copy(out, headers)

	if metadata.EventID != "" {
		out[HeaderEventID] = metadata.EventID
	}

	if metadata.EventName != "" {
		out[HeaderEventName] = metadata.EventName
	}

	if metadata.Producer != "" {
		out[HeaderProducer] = metadata.Producer
	}

	if !metadata.OccurredAt.IsZero() {
		out[HeaderOccurredAt] = metadata.OccurredAt.UTC().Format(time.RFC3339Nano)
	}

	if metadata.CorrelationID != "" {
		out[HeaderCorrelationID] = metadata.CorrelationID
	}

	if metadata.CausationID != "" {
		out[HeaderCausationID] = metadata.CausationID
	}

	if metadata.SchemaVersion != "" {
		out[HeaderSchemaVersion] = metadata.SchemaVersion
	}

	return out
}

func MetadataFromHeaders(headers map[string]string) EventMetadata {
	metadata := EventMetadata{}

	metadata.EventID = headers[HeaderEventID]
	metadata.EventName = headers[HeaderEventName]
	metadata.Producer = headers[HeaderProducer]
	metadata.CorrelationID = headers[HeaderCorrelationID]
	metadata.CausationID = headers[HeaderCausationID]
	metadata.SchemaVersion = headers[HeaderSchemaVersion]

	if occurredAt, ok := headers[HeaderOccurredAt]; ok {
		if parsed, err := time.Parse(time.RFC3339Nano, occurredAt); err == nil {
			metadata.OccurredAt = parsed
		}
	}

	return metadata
}

func MetadataFromProto(protoMetadata *commonv1.EventMetadata) EventMetadata {
	if protoMetadata == nil {
		return EventMetadata{}
	}

	metadata := EventMetadata{
		EventID:       protoMetadata.GetEventId(),
		EventName:     protoMetadata.GetEventName(),
		Producer:      protoMetadata.GetProducer(),
		CorrelationID: protoMetadata.GetCorrelationId(),
		CausationID:   protoMetadata.GetCausationId(),
		SchemaVersion: protoMetadata.GetSchemaVersion(),
	}

	if protoMetadata.GetOccurredAt() != nil {
		metadata.OccurredAt = protoMetadata.GetOccurredAt().AsTime().UTC()
	}

	return metadata
}

func MetadataToProto(metadata EventMetadata) *commonv1.EventMetadata {
	protoMetadata := &commonv1.EventMetadata{
		EventId:       metadata.EventID,
		EventName:     metadata.EventName,
		Producer:      metadata.Producer,
		CorrelationId: metadata.CorrelationID,
		CausationId:   metadata.CausationID,
		SchemaVersion: metadata.SchemaVersion,
	}

	if !metadata.OccurredAt.IsZero() {
		protoMetadata.OccurredAt = timestamppb.New(metadata.OccurredAt.UTC())
	}

	return protoMetadata
}

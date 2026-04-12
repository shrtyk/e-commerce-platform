package events

import (
	"context"
	"errors"
	"testing"
	"time"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type fakeOutboxRepository struct {
	err    error
	record commonoutbox.Record
}

func (f *fakeOutboxRepository) Append(_ context.Context, record commonoutbox.Record) (commonoutbox.Record, error) {
	f.record = record

	if f.err != nil {
		return commonoutbox.Record{}, f.err
	}

	return record, nil
}

func TestEventPublisherPublish(t *testing.T) {
	tests := []struct {
		name        string
		event       domain.DomainEvent
		producerErr error
		errContains string
	}{
		{
			name: "success",
			event: domain.DomainEvent{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				Producer:      "catalog-svc",
				OccurredAt:    time.Now().UTC(),
				CorrelationID: "corr-1",
				CausationID:   "cause-1",
				SchemaVersion: "1",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.product.events",
				Key:           "product-1",
				Payload: domain.ProductCreatedPayload{
					ProductID:  "product-1",
					SKU:        "SKU-1",
					Name:       "Sneakers",
					Status:     domain.ProductStatusPublished,
					Price:      1234,
					Currency:   "USD",
					CategoryID: "",
				},
				Headers: map[string]string{"event_name": "catalog.product.created"},
			},
		},
		{
			name: "unsupported event",
			event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) {
				event.EventName = "catalog.product.unknown"
			}),
			errContains: "unsupported event name",
		},
		{
			name: "invalid payload type",
			event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) {
				event.Payload = "bad payload"
			}),
			errContains: "invalid product created payload type",
		},
		{
			name:        "produce error",
			event:       validEventForValidation(),
			producerErr: errors.New("broker unavailable"),
			errContains: "append outbox record",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeOutboxRepository{err: tt.producerErr}
			publisher, err := NewEventPublisher(fake)
			require.NoError(t, err)

			err = publisher.Publish(context.Background(), tt.event)
			if tt.errContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, "catalog.product.events", fake.record.Topic)
			require.Equal(t, []byte("product-1"), fake.record.Key)
			require.NotEmpty(t, fake.record.Payload)
			require.Equal(t, commonoutbox.StatusPending, fake.record.Status)

			var payload catalogv1.ProductCreated
			err = proto.Unmarshal(fake.record.Payload, &payload)
			require.NoError(t, err)
			require.NotNil(t, payload.Metadata)
			require.Equal(t, tt.event.EventID, payload.Metadata.EventId)
			require.Equal(t, tt.event.EventName, payload.Metadata.EventName)
			require.Equal(t, tt.event.Producer, payload.Metadata.Producer)
			require.Equal(t, tt.event.CorrelationID, payload.Metadata.CorrelationId)
			require.Equal(t, tt.event.CausationID, payload.Metadata.CausationId)
			require.Equal(t, tt.event.SchemaVersion, payload.Metadata.SchemaVersion)
			require.Equal(t, tt.event.EventID, fake.record.Headers[commonkafka.HeaderEventID])
			require.Equal(t, tt.event.EventName, fake.record.Headers[commonkafka.HeaderEventName])
			require.Equal(t, tt.event.Producer, fake.record.Headers[commonkafka.HeaderProducer])
			require.Equal(t, tt.event.OccurredAt.UTC().Format(time.RFC3339Nano), fake.record.Headers[commonkafka.HeaderOccurredAt])
			require.Equal(t, tt.event.CorrelationID, fake.record.Headers[commonkafka.HeaderCorrelationID])
			require.Equal(t, tt.event.CausationID, fake.record.Headers[commonkafka.HeaderCausationID])
			require.Equal(t, tt.event.SchemaVersion, fake.record.Headers[commonkafka.HeaderSchemaVersion])
			require.Equal(t, "ecommerce.catalog.v1.ProductCreated", fake.record.Headers[commonkafka.HeaderRecordName])

			headersMetadata := commonkafka.MetadataFromHeaders(fake.record.Headers)
			require.Equal(t, payload.Metadata.EventId, headersMetadata.EventID)
			require.Equal(t, payload.Metadata.EventName, headersMetadata.EventName)
			require.Equal(t, payload.Metadata.Producer, headersMetadata.Producer)
			require.Equal(t, payload.Metadata.CorrelationId, headersMetadata.CorrelationID)
			require.Equal(t, payload.Metadata.CausationId, headersMetadata.CausationID)
			require.Equal(t, payload.Metadata.SchemaVersion, headersMetadata.SchemaVersion)
			require.Equal(t, payload.Metadata.OccurredAt.AsTime().UTC(), headersMetadata.OccurredAt.UTC())
			require.Equal(t, "product-1", payload.ProductId)
			require.Equal(t, "SKU-1", payload.Sku)
			require.Equal(t, "Sneakers", payload.Name)
			require.Equal(t, catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED, payload.Status)
			require.NotNil(t, payload.Price)
			require.Equal(t, int64(1234), payload.Price.Amount)
			require.Equal(t, "USD", payload.Price.Currency)
		})
	}
}

func TestEventPublisherPublishEnvelopeValidation(t *testing.T) {
	tests := []struct {
		name        string
		event       domain.DomainEvent
		errContains string
	}{
		{name: "missing event_id", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.EventID = "" }), errContains: "event_id is required"},
		{name: "missing event_name", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.EventName = "" }), errContains: "event_name is required"},
		{name: "missing producer", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Producer = "" }), errContains: "producer is required"},
		{name: "missing occurred_at", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.OccurredAt = time.Time{} }), errContains: "occurred_at is required"},
		{name: "missing schema_version", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.SchemaVersion = "" }), errContains: "schema_version is required"},
		{name: "missing topic", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Topic = "" }), errContains: "topic is required"},
		{name: "missing key", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Key = "" }), errContains: "key is required"},
		{name: "missing payload", event: withEvent(validEventForValidation(), func(event *domain.DomainEvent) { event.Payload = nil }), errContains: "payload is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeOutboxRepository{}
			publisher, err := NewEventPublisher(fake)
			require.NoError(t, err)

			err = publisher.Publish(context.Background(), tt.event)
			require.Error(t, err)
			require.ErrorContains(t, err, "validate event envelope")
			require.ErrorContains(t, err, tt.errContains)
			require.Empty(t, fake.record)
		})
	}
}

func validEventForValidation() domain.DomainEvent {
	return domain.DomainEvent{
		EventID:       "evt-1",
		EventName:     "catalog.product.created",
		Producer:      "catalog-svc",
		OccurredAt:    time.Now().UTC(),
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		SchemaVersion: "1",
		Topic:         "catalog.product.events",
		Key:           "product-1",
		Payload: domain.ProductCreatedPayload{
			ProductID: "product-1",
		},
	}
}

func withEvent(event domain.DomainEvent, mutate func(event *domain.DomainEvent)) domain.DomainEvent {
	mutate(&event)
	return event
}

func TestNewEventPublisher(t *testing.T) {
	_, err := NewEventPublisher(nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "outbox repository is nil")
}

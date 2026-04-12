package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"github.com/twmb/franz-go/pkg/sr/srfake"
	"google.golang.org/protobuf/proto"
)

type relayProducerStub struct {
	produceSyncFunc func(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

func (s *relayProducerStub) ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults {
	if s.produceSyncFunc == nil {
		return kgo.ProduceResults{{Record: records[0]}}
	}

	return s.produceSyncFunc(ctx, records...)
}

func TestNewPublisher(t *testing.T) {
	registry := srfake.New()
	t.Cleanup(registry.Close)

	registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
	require.NoError(t, err)

	_, err = NewPublisher(nil, registryClient)
	require.Error(t, err)
	require.ErrorContains(t, err, "kafka producer is nil")

	_, err = NewPublisher(&relayProducerStub{}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "schema registry client is nil")

	publisher, err := NewPublisher(&relayProducerStub{}, registryClient)
	require.NoError(t, err)
	require.NotNil(t, publisher)
}

func TestPublisherPublish(t *testing.T) {
	now := time.Date(2026, time.April, 12, 15, 4, 5, 123000000, time.UTC)

	recordPayload := &catalogv1.ProductCreated{
		Metadata: commonkafka.MetadataToProto(commonkafka.EventMetadata{
			EventID:       "evt-1",
			EventName:     "catalog.product.created",
			Producer:      "product-svc",
			OccurredAt:    now,
			CorrelationID: "corr-1",
			CausationID:   "cause-1",
			SchemaVersion: "1",
		}),
		ProductId: "product-1",
		Sku:       "SKU-1",
		Name:      "Sneakers",
	}

	rawPayload, err := proto.Marshal(recordPayload)
	require.NoError(t, err)

	baseHeaders := map[string]string{
		commonkafka.HeaderEventID:       "evt-1",
		commonkafka.HeaderEventName:     "catalog.product.created",
		commonkafka.HeaderProducer:      "product-svc",
		commonkafka.HeaderOccurredAt:    now.Format(time.RFC3339Nano),
		commonkafka.HeaderCorrelationID: "corr-1",
		commonkafka.HeaderCausationID:   "cause-1",
		commonkafka.HeaderSchemaVersion: "1",
		commonkafka.HeaderRecordName:    productCreatedRecordName,
	}

	tests := []struct {
		name        string
		record      commonoutbox.Record
		produceErr  error
		errContains string
		assertFn    func(t *testing.T, produced *kgo.Record)
	}{
		{
			name: "relay publishes schema-registry wire format decodable by common serde",
			record: commonoutbox.Record{
				Topic:   "catalog.product.events",
				Key:     []byte("product-1"),
				Payload: rawPayload,
				Headers: cloneHeaders(baseHeaders),
			},
			assertFn: func(t *testing.T, produced *kgo.Record) {
				require.NotNil(t, produced)
				require.NotEqual(t, rawPayload, produced.Value)

				headers := recordHeadersToMap(produced.Headers)
				require.Equal(t, "evt-1", headers[commonkafka.HeaderEventID])
				require.Equal(t, "catalog.product.created", headers[commonkafka.HeaderEventName])
				require.Equal(t, "product-svc", headers[commonkafka.HeaderProducer])
				require.Equal(t, now.Format(time.RFC3339Nano), headers[commonkafka.HeaderOccurredAt])
				require.Equal(t, "corr-1", headers[commonkafka.HeaderCorrelationID])
				require.Equal(t, "cause-1", headers[commonkafka.HeaderCausationID])
				require.Equal(t, "1", headers[commonkafka.HeaderSchemaVersion])
				require.Equal(t, productCreatedRecordName, headers[commonkafka.HeaderRecordName])

				registry := srfake.New()
				t.Cleanup(registry.Close)

				registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
				require.NoError(t, err)

				decodeSerde := commonkafka.NewProtoSerde(registryClient, commonkafka.NewDescriptorSchemaProvider())
				err = decodeSerde.RegisterType(context.Background(), "catalog.product.events", &catalogv1.ProductCreated{})
				require.NoError(t, err)

				decoded, err := decodeSerde.Decode(produced.Value)
				require.NoError(t, err)

				decodedProduct, ok := decoded.(*catalogv1.ProductCreated)
				require.True(t, ok)
				require.Equal(t, "product-1", decodedProduct.GetProductId())
				decodedMetadata := commonkafka.MetadataFromProto(decodedProduct.GetMetadata())
				require.Equal(t, headers[commonkafka.HeaderEventID], decodedMetadata.EventID)
				require.Equal(t, headers[commonkafka.HeaderEventName], decodedMetadata.EventName)
				require.Equal(t, headers[commonkafka.HeaderProducer], decodedMetadata.Producer)
				require.Equal(t, headers[commonkafka.HeaderCorrelationID], decodedMetadata.CorrelationID)
				require.Equal(t, headers[commonkafka.HeaderCausationID], decodedMetadata.CausationID)
				require.Equal(t, headers[commonkafka.HeaderSchemaVersion], decodedMetadata.SchemaVersion)
				require.Equal(t, now.UTC(), decodedMetadata.OccurredAt.UTC())
			},
		},
		{
			name: "missing topic",
			record: commonoutbox.Record{
				Payload: rawPayload,
				Headers: cloneHeaders(baseHeaders),
			},
			errContains: "record topic is required",
		},
		{
			name: "missing payload",
			record: commonoutbox.Record{
				Topic:   "catalog.product.events",
				Headers: cloneHeaders(baseHeaders),
			},
			errContains: "record payload is required",
		},
		{
			name: "unsupported record name",
			record: commonoutbox.Record{
				Topic:   "catalog.product.events",
				Payload: rawPayload,
				Headers: map[string]string{
					commonkafka.HeaderRecordName: "ecommerce.catalog.v1.Unknown",
				},
			},
			errContains: "unsupported record name",
		},
		{
			name: "kafka error",
			record: commonoutbox.Record{
				Topic:   "catalog.product.events",
				Key:     []byte("product-1"),
				Payload: rawPayload,
				Headers: cloneHeaders(baseHeaders),
			},
			produceErr:  errors.New("broker unavailable"),
			errContains: "publish proto via common producer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := srfake.New()
			t.Cleanup(registry.Close)

			registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
			require.NoError(t, err)

			var produced *kgo.Record
			publisher, err := NewPublisher(&relayProducerStub{produceSyncFunc: func(_ context.Context, records ...*kgo.Record) kgo.ProduceResults {
				require.Len(t, records, 1)
				produced = records[0]
				return kgo.ProduceResults{{Record: records[0], Err: tt.produceErr}}
			}}, registryClient)
			require.NoError(t, err)

			err = publisher.Publish(context.Background(), tt.record)
			if tt.errContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, produced)
			require.Equal(t, tt.record.Topic, produced.Topic)
			require.Equal(t, tt.record.Key, produced.Key)

			if tt.assertFn != nil {
				tt.assertFn(t, produced)
			}
		})
	}
}

func recordHeadersToMap(headers []kgo.RecordHeader) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(headers))
	for _, header := range headers {
		out[header.Key] = string(header.Value)
	}

	return out
}

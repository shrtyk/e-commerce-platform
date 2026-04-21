package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"github.com/twmb/franz-go/pkg/sr/srfake"
	"google.golang.org/protobuf/proto"

	notificationv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/notification/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
)

const testNotificationSentRecordName = "ecommerce.notification.v1.NotificationSent"

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

	_, err = NewPublisher(nil, registryClient, commonkafka.NewTypeRegistry())
	require.Error(t, err)
	require.ErrorContains(t, err, "kafka producer is nil")

	typeRegistry := commonkafka.NewTypeRegistry()

	_, err = NewPublisher(&relayProducerStub{}, nil, typeRegistry)
	require.Error(t, err)
	require.ErrorContains(t, err, "schema registry client is nil")

	_, err = NewPublisher(&relayProducerStub{}, registryClient, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "type registry is nil")

	err = typeRegistry.RegisterMessages(
		&notificationv1.DeliveryRequested{},
		&notificationv1.NotificationSent{},
		&notificationv1.NotificationFailed{},
	)
	require.NoError(t, err)

	publisher, err := NewPublisher(&relayProducerStub{}, registryClient, typeRegistry)
	require.NoError(t, err)
	require.NotNil(t, publisher)
}

func TestPublisherPublish(t *testing.T) {
	now := time.Date(2026, time.April, 21, 10, 15, 0, 0, time.UTC)

	recordPayload := &notificationv1.NotificationSent{
		Metadata: commonkafka.MetadataToProto(commonkafka.EventMetadata{
			EventID:       "evt-1",
			EventName:     "notification.sent",
			Producer:      "notification-svc",
			OccurredAt:    now,
			CorrelationID: "corr-1",
			CausationID:   "cause-1",
			SchemaVersion: "1",
		}),
		DeliveryRequestId: "req-1",
		SourceEventName:   "order.confirmed",
		Channel:           "email",
		Recipient:         "user@example.com",
		Status:            notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT,
	}

	rawPayload, err := proto.Marshal(recordPayload)
	require.NoError(t, err)

	baseHeaders := map[string]string{
		commonkafka.HeaderEventID:       "evt-1",
		commonkafka.HeaderEventName:     "notification.sent",
		commonkafka.HeaderProducer:      "notification-svc",
		commonkafka.HeaderOccurredAt:    now.Format(time.RFC3339Nano),
		commonkafka.HeaderCorrelationID: "corr-1",
		commonkafka.HeaderCausationID:   "cause-1",
		commonkafka.HeaderSchemaVersion: "1",
		commonkafka.HeaderRecordName:    testNotificationSentRecordName,
	}

	tests := []struct {
		name        string
		record      commonoutbox.Record
		produceErr  error
		errContains string
		assertFn    func(t *testing.T, produced *kgo.Record)
	}{
		{
			name: "relay publishes wire format decodable by common serde",
			record: commonoutbox.Record{
				Topic:   "notification.events",
				Key:     []byte("req-1"),
				Payload: rawPayload,
				Headers: cloneHeaders(baseHeaders),
			},
			assertFn: func(t *testing.T, produced *kgo.Record) {
				require.NotNil(t, produced)
				require.NotEqual(t, rawPayload, produced.Value)

				headers := recordHeadersToMap(produced.Headers)
				require.Equal(t, "evt-1", headers[commonkafka.HeaderEventID])
				require.Equal(t, "notification.sent", headers[commonkafka.HeaderEventName])
				require.Equal(t, testNotificationSentRecordName, headers[commonkafka.HeaderRecordName])

				registry := srfake.New()
				t.Cleanup(registry.Close)

				registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
				require.NoError(t, err)

				decodeSerde := commonkafka.NewProtoSerde(registryClient, commonkafka.NewDescriptorSchemaProvider())
				err = decodeSerde.RegisterType(context.Background(), "notification.events", &notificationv1.NotificationSent{})
				require.NoError(t, err)

				decoded, err := decodeSerde.Decode(produced.Value)
				require.NoError(t, err)

				decodedSent, ok := decoded.(*notificationv1.NotificationSent)
				require.True(t, ok)
				require.Equal(t, "req-1", decodedSent.GetDeliveryRequestId())
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
				Topic:   "notification.events",
				Headers: cloneHeaders(baseHeaders),
			},
			errContains: "record payload is required",
		},
		{
			name: "unsupported record name",
			record: commonoutbox.Record{
				Topic:   "notification.events",
				Payload: rawPayload,
				Headers: map[string]string{
					commonkafka.HeaderRecordName: "ecommerce.notification.v1.Unknown",
				},
			},
			errContains: "resolve message type",
		},
		{
			name: "kafka error",
			record: commonoutbox.Record{
				Topic:   "notification.events",
				Key:     []byte("req-1"),
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

			typeRegistry := commonkafka.NewTypeRegistry()
			err = typeRegistry.RegisterMessages(
				&notificationv1.DeliveryRequested{},
				&notificationv1.NotificationSent{},
				&notificationv1.NotificationFailed{},
			)
			require.NoError(t, err)

			var produced *kgo.Record
			publisher, err := NewPublisher(&relayProducerStub{produceSyncFunc: func(_ context.Context, records ...*kgo.Record) kgo.ProduceResults {
				require.Len(t, records, 1)
				produced = records[0]
				return kgo.ProduceResults{{Record: records[0], Err: tt.produceErr}}
			}}, registryClient, typeRegistry)
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

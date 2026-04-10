package kafka

import (
	"context"
	"errors"
	"fmt"
	"testing"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/twmb/franz-go/pkg/sr"
	"github.com/twmb/franz-go/pkg/sr/srfake"
)

type fakeConsumerClient struct {
	fetches kgo.Fetches
}

func (f *fakeConsumerClient) PollFetches(_ context.Context) kgo.Fetches {
	return f.fetches
}

func TestConsumerPollDecodesEnvelopeAndMessage(t *testing.T) {
	registry := srfake.New()
	t.Cleanup(registry.Close)

	registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
	require.NoError(t, err)

	serde := NewProtoSerde(registryClient, staticSchemaProvider{})
	ctx := context.Background()

	original := &catalogv1.ProductCreated{ProductId: "product-1", Name: "Sneakers"}
	encoded, recordName, err := serde.Encode(ctx, "catalog.product.events", original)
	require.NoError(t, err)

	headers := headersFromMap(map[string]string{
		HeaderEventID:       "evt-1",
		HeaderEventName:     "catalog.product.created",
		HeaderProducer:      "catalog-svc",
		HeaderCorrelationID: "corr-1",
		HeaderSchemaVersion: "v1",
		HeaderRecordName:    recordName,
	})

	fetches := kgo.Fetches{
		kgo.Fetch{
			Topics: []kgo.FetchTopic{
				{
					Topic: "catalog.product.events",
					Partitions: []kgo.FetchPartition{
						{Partition: 0, Records: []*kgo.Record{{Topic: "catalog.product.events", Key: []byte("product-1"), Value: encoded, Headers: headers}}},
					},
				},
			},
		},
	}

	consumer, err := NewConsumer(&fakeConsumerClient{fetches: fetches}, serde)
	require.NoError(t, err)

	got, err := consumer.Poll(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "catalog.product.events", got[0].Envelope.Topic)
	require.Equal(t, "evt-1", got[0].Envelope.Metadata.EventID)

	decoded, ok := got[0].Message.(*catalogv1.ProductCreated)
	require.True(t, ok)
	require.Equal(t, "product-1", decoded.GetProductId())
}

func TestConsumerPollFetchErrorClassification(t *testing.T) {
	serde := newTestSerde(t)

	tests := []struct {
		name             string
		fetches          kgo.Fetches
		wantRetriable    bool
		wantNonRetriable bool
		wantErrContains  string
	}{
		{
			name:             "retriable fetch error",
			fetches:          kgo.NewErrFetch(kerr.LeaderNotAvailable),
			wantRetriable:    true,
			wantNonRetriable: false,
			wantErrContains:  "poll fetches",
		},
		{
			name:             "non-retriable fetch error",
			fetches:          kgo.NewErrFetch(kerr.InvalidTopicException),
			wantRetriable:    false,
			wantNonRetriable: true,
			wantErrContains:  "poll fetches",
		},
		{
			name:             "context canceled",
			fetches:          kgo.NewErrFetch(context.Canceled),
			wantRetriable:    false,
			wantNonRetriable: true,
			wantErrContains:  context.Canceled.Error(),
		},
		{
			name:             "client closed",
			fetches:          kgo.NewErrFetch(kgo.ErrClientClosed),
			wantRetriable:    false,
			wantNonRetriable: true,
			wantErrContains:  kgo.ErrClientClosed.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer, err := NewConsumer(&fakeConsumerClient{fetches: tt.fetches}, serde)
			require.NoError(t, err)

			got, err := consumer.Poll(context.Background())
			require.Nil(t, got)
			require.Error(t, err)
			require.Equal(t, tt.wantRetriable, IsRetriable(err))
			require.Equal(t, tt.wantNonRetriable, IsNonRetriable(err))
			require.ErrorContains(t, err, tt.wantErrContains)
		})
	}
}

func TestConsumerPollDecodeErrors(t *testing.T) {
	registry := srfake.New()
	t.Cleanup(registry.Close)

	registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
	require.NoError(t, err)

	producerSerde := NewProtoSerde(registryClient, staticSchemaProvider{})
	ctx := context.Background()

	original := &catalogv1.ProductCreated{ProductId: "product-1", Name: "Sneakers"}
	encoded, recordName, err := producerSerde.Encode(ctx, "catalog.product.events", original)
	require.NoError(t, err)

	tests := []struct {
		name            string
		record          *kgo.Record
		consumerSerde   *ProtoSerde
		wantErrContains string
		wantErrIs       error
	}{
		{
			name: "unregistered schema with recordName header",
			record: &kgo.Record{
				Topic: "catalog.product.events",
				Value: encoded,
				Headers: []kgo.RecordHeader{
					{Key: HeaderRecordName, Value: []byte(recordName)},
				},
			},
			consumerSerde:   NewProtoSerde(registryClient, staticSchemaProvider{}),
			wantErrContains: fmt.Sprintf("schema id is not registered locally for record %s", recordName),
			wantErrIs:       sr.ErrNotRegistered,
		},
		{
			name: "decode payload failure",
			record: &kgo.Record{
				Topic:   "catalog.product.events",
				Value:   []byte("not-confluent-wire-format"),
				Headers: nil,
			},
			consumerSerde:   producerSerde,
			wantErrContains: "decode record topic=catalog.product.events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetches := kgo.Fetches{{
				Topics: []kgo.FetchTopic{{
					Topic: "catalog.product.events",
					Partitions: []kgo.FetchPartition{{
						Partition: 0,
						Records:   []*kgo.Record{tt.record},
					}},
				}},
			}}

			consumer, err := NewConsumer(&fakeConsumerClient{fetches: fetches}, tt.consumerSerde)
			require.NoError(t, err)

			got, err := consumer.Poll(context.Background())
			require.Nil(t, got)
			require.Error(t, err)
			require.True(t, IsNonRetriable(err))
			require.ErrorContains(t, err, tt.wantErrContains)
			if tt.wantErrIs != nil {
				require.True(t, errors.Is(err, tt.wantErrIs))
			}
		})
	}
}

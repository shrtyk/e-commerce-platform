package kafka

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"github.com/twmb/franz-go/pkg/sr/srfake"
	"google.golang.org/protobuf/proto"
)

type fakeProducerClient struct {
	errs      []error
	callCount int
	records   []*kgo.Record
}

func (f *fakeProducerClient) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	f.callCount++
	f.records = append(f.records, rs...)

	var err error
	if len(f.errs) >= f.callCount {
		err = f.errs[f.callCount-1]
	}

	return kgo.ProduceResults{{
		Record: rs[0],
		Err:    err,
	}}
}

type staticSchemaProvider struct{}

func (staticSchemaProvider) SchemaFor(message proto.Message) (SchemaDefinition, error) {
	fullName := message.ProtoReflect().Descriptor().FullName()
	return SchemaDefinition{
		Schema: fmt.Sprintf("syntax = \"proto3\"; message %s { string value = 1; }", fullName.Name()),
		Index:  []int{0},
	}, nil
}

func newTestSerde(t *testing.T) *ProtoSerde {
	t.Helper()

	registry := srfake.New()
	t.Cleanup(registry.Close)

	registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
	require.NoError(t, err)

	return NewProtoSerde(registryClient, staticSchemaProvider{})
}

func TestProducerPublishRetryBehavior(t *testing.T) {
	serde := newTestSerde(t)
	retryPolicy := RetryPolicy{MaxAttempts: 3, Backoff: time.Millisecond, Multiplier: 1, MaxBackoff: time.Millisecond}

	tests := []struct {
		name             string
		errs             []error
		wantAttempts     int
		wantRetriable    bool
		wantNonRetriable bool
		wantErr          bool
	}{
		{
			name:             "retry then success",
			errs:             []error{kerr.LeaderNotAvailable, nil},
			wantAttempts:     2,
			wantRetriable:    false,
			wantNonRetriable: false,
			wantErr:          false,
		},
		{
			name:             "stop on non-retriable",
			errs:             []error{kerr.InvalidTopicException},
			wantAttempts:     1,
			wantRetriable:    false,
			wantNonRetriable: true,
			wantErr:          true,
		},
		{
			name:             "exhaust retriable",
			errs:             []error{kerr.LeaderNotAvailable, kerr.LeaderNotAvailable, kerr.LeaderNotAvailable},
			wantAttempts:     3,
			wantRetriable:    true,
			wantNonRetriable: false,
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeProducerClient{errs: tt.errs}
			producer, err := NewProducer(client, serde, retryPolicy)
			require.NoError(t, err)
			producer.timer = func(_ time.Duration) <-chan time.Time {
				ch := make(chan time.Time, 1)
				ch <- time.Now()
				return ch
			}

			err = producer.PublishProto(
				context.Background(),
				"catalog.product.events",
				[]byte("product-1"),
				map[string]string{"x-source": "test"},
				EventMetadata{EventID: "evt-1", EventName: "catalog.product.created", Producer: "catalog-svc"},
				&catalogv1.ProductCreated{ProductId: "product-1", Name: "Sneakers"},
			)

			require.Equal(t, tt.wantErr, err != nil)
			require.Equal(t, tt.wantAttempts, client.callCount)

			if tt.wantErr {
				require.Equal(t, tt.wantRetriable, IsRetriable(err))
				require.Equal(t, tt.wantNonRetriable, IsNonRetriable(err))
			}

			require.NotEmpty(t, client.records)
			headers := headersToMap(client.records[0].Headers)
			require.Equal(t, "evt-1", headers[HeaderEventID])
			require.Equal(t, "catalog.product.created", headers[HeaderEventName])
			require.Equal(t, "ecommerce.catalog.v1.ProductCreated", headers[HeaderRecordName])
		})
	}
}

func TestProducerPublishValidation(t *testing.T) {
	serde := newTestSerde(t)
	client := &fakeProducerClient{}
	producer, err := NewProducer(client, serde, DefaultRetryPolicy())
	require.NoError(t, err)

	err = producer.Publish(context.Background(), EventEnvelope{Topic: "", Payload: []byte("x")})
	require.Error(t, err)
	require.True(t, IsNonRetriable(err))

	err = producer.Publish(context.Background(), EventEnvelope{Topic: "topic", Payload: nil})
	require.Error(t, err)
	require.True(t, IsNonRetriable(err))
}

func TestProducerPublishContextDoneDuringRetry(t *testing.T) {
	serde := newTestSerde(t)
	client := &fakeProducerClient{errs: []error{kerr.LeaderNotAvailable}}
	producer, err := NewProducer(client, serde, RetryPolicy{MaxAttempts: 2, Backoff: time.Second, Multiplier: 1, MaxBackoff: time.Second})
	require.NoError(t, err)

	producer.timer = func(_ time.Duration) <-chan time.Time {
		return make(chan time.Time)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = producer.Publish(ctx, EventEnvelope{Topic: "topic", Payload: []byte("payload")})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
	require.True(t, IsNonRetriable(err))
}

func TestProducerPublishProtoEncodeErrorClassification(t *testing.T) {
	retryPolicy := RetryPolicy{MaxAttempts: 2, Backoff: time.Millisecond, Multiplier: 1, MaxBackoff: time.Millisecond}

	tests := []struct {
		name             string
		serde            *ProtoSerde
		wantRetriable    bool
		wantNonRetriable bool
		wantErrContains  string
	}{
		{
			name:             "non-retriable encode error",
			serde:            NewProtoSerde(nil, staticSchemaProvider{}),
			wantRetriable:    false,
			wantNonRetriable: true,
			wantErrContains:  "publish proto: encode payload",
		},
		{
			name: "retriable encode error from registration",
			serde: NewProtoSerde(
				failingRegistry{err: kerr.LeaderNotAvailable},
				staticSchemaProvider{},
			),
			wantRetriable:    true,
			wantNonRetriable: false,
			wantErrContains:  "publish proto: encode payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			producer, err := NewProducer(&fakeProducerClient{}, tt.serde, retryPolicy)
			require.NoError(t, err)

			err = producer.PublishProto(
				context.Background(),
				"catalog.product.events",
				[]byte("product-1"),
				nil,
				EventMetadata{},
				&catalogv1.ProductCreated{ProductId: "product-1", Name: "Sneakers"},
			)

			require.Error(t, err)
			require.Equal(t, tt.wantRetriable, IsRetriable(err))
			require.Equal(t, tt.wantNonRetriable, IsNonRetriable(err))
			require.ErrorContains(t, err, tt.wantErrContains)
		})
	}
}

type failingRegistry struct {
	err error
}

func (f failingRegistry) CreateSchema(_ context.Context, _ string, _ sr.Schema) (sr.SubjectSchema, error) {
	return sr.SubjectSchema{}, f.err
}

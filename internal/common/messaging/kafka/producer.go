package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

type producerClient interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
}

type Producer struct {
	client producerClient
	serde  *ProtoSerde
	retry  RetryPolicy
	clock  func() time.Time
	timer  func(time.Duration) <-chan time.Time
}

func NewProducer(client producerClient, serde *ProtoSerde, retry RetryPolicy) (*Producer, error) {
	if client == nil {
		return nil, fmt.Errorf("producer client is nil")
	}

	if serde == nil {
		return nil, fmt.Errorf("proto serde is nil")
	}

	if err := retry.Validate(); err != nil {
		return nil, fmt.Errorf("validate retry policy: %w", err)
	}

	return &Producer{
		client: client,
		serde:  serde,
		retry:  retry,
		clock:  time.Now,
		timer:  time.After,
	}, nil
}

func (p *Producer) PublishProto(ctx context.Context, topic string, key []byte, headers map[string]string, metadata EventMetadata, message proto.Message) error {
	if message == nil {
		return wrapNonRetriable(fmt.Errorf("message is nil"), "publish proto")
	}

	payload, recordName, err := p.serde.Encode(ctx, topic, message)
	if err != nil {
		if IsRetriable(err) {
			return wrapRetriable(fmt.Errorf("encode payload: %w", err), "publish proto")
		}

		return wrapNonRetriable(fmt.Errorf("encode payload: %w", err), "publish proto")
	}

	headersWithMetadata := MetadataToHeaders(metadata, headers)
	headersWithMetadata[HeaderRecordName] = recordName

	envelope := EventEnvelope{
		Topic:    topic,
		Key:      key,
		Headers:  headersWithMetadata,
		Payload:  payload,
		Metadata: metadata,
	}

	return p.Publish(ctx, envelope)
}

func (p *Producer) Publish(ctx context.Context, envelope EventEnvelope) error {
	if envelope.Topic == "" {
		return wrapNonRetriable(fmt.Errorf("topic is empty"), "publish event")
	}

	if len(envelope.Payload) == 0 {
		return wrapNonRetriable(fmt.Errorf("payload is empty"), "publish event")
	}

	record := &kgo.Record{
		Topic:   envelope.Topic,
		Key:     envelope.Key,
		Value:   envelope.Payload,
		Headers: headersFromMap(MetadataToHeaders(envelope.Metadata, envelope.Headers)),
	}

	var lastErr error
	for attempt := 1; attempt <= p.retry.MaxAttempts; attempt++ {
		if attempt > 1 {
			wait := p.retry.BackoffForAttempt(attempt)
			if wait > 0 {
				select {
				case <-ctx.Done():
					return wrapNonRetriable(fmt.Errorf("context done before retry: %w", ctx.Err()), "publish event")
				case <-p.timer(wait):
				}
			}
		}

		results := p.client.ProduceSync(ctx, record)
		err := results.FirstErr()
		if err == nil {
			return nil
		}

		classified := ClassifyError(err)
		lastErr = classified
		if !IsRetriable(classified) {
			return wrapNonRetriable(fmt.Errorf("attempt %d/%d: %w", attempt, p.retry.MaxAttempts, err), "publish event")
		}
	}

	if lastErr == nil {
		return wrapRetriable(fmt.Errorf("unknown produce failure"), "publish event")
	}

	return wrapRetriable(fmt.Errorf("exhausted retries: %w", lastErr), "publish event")
}

func headersFromMap(headers map[string]string) []kgo.RecordHeader {
	if len(headers) == 0 {
		return nil
	}

	out := make([]kgo.RecordHeader, 0, len(headers))
	for key, value := range headers {
		out = append(out, kgo.RecordHeader{Key: key, Value: []byte(value)})
	}

	return out
}

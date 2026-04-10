package kafka

import (
	"context"
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
)

type consumerClient interface {
	PollFetches(ctx context.Context) kgo.Fetches
}

type ConsumedMessage struct {
	Envelope EventEnvelope
	Message  proto.Message
}

type Consumer struct {
	client consumerClient
	serde  *ProtoSerde
}

func NewConsumer(client consumerClient, serde *ProtoSerde) (*Consumer, error) {
	if client == nil {
		return nil, fmt.Errorf("consumer client is nil")
	}

	if serde == nil {
		return nil, fmt.Errorf("proto serde is nil")
	}

	return &Consumer{client: client, serde: serde}, nil
}

func (c *Consumer) Poll(ctx context.Context) ([]ConsumedMessage, error) {
	fetches := c.client.PollFetches(ctx)
	if fetches.IsClientClosed() {
		return nil, wrapNonRetriable(kgo.ErrClientClosed, "poll fetches")
	}

	if err := fetches.Err(); err != nil {
		if errors.Is(err, kgo.ErrClientClosed) {
			return nil, wrapNonRetriable(err, "poll fetches")
		}

		classified := ClassifyError(err)
		if IsRetriable(classified) {
			return nil, wrapRetriable(err, "poll fetches")
		}

		return nil, wrapNonRetriable(err, "poll fetches")
	}

	records := fetches.Records()
	out := make([]ConsumedMessage, 0, len(records))
	for _, record := range records {
		headers := headersToMap(record.Headers)
		envelope := EventEnvelope{
			Topic:    record.Topic,
			Key:      record.Key,
			Headers:  headers,
			Payload:  record.Value,
			Metadata: MetadataFromHeaders(headers),
		}

		message, err := c.serde.Decode(record.Value)
		recordName := headers[HeaderRecordName]
		if err != nil && errors.Is(err, sr.ErrNotRegistered) && recordName != "" {
			return nil, wrapNonRetriable(fmt.Errorf("decode record topic=%s partition=%d offset=%d: schema id is not registered locally for record %s: %w", record.Topic, record.Partition, record.Offset, recordName, err), "poll fetches")
		}

		if err != nil {
			return nil, wrapNonRetriable(fmt.Errorf("decode record topic=%s partition=%d offset=%d: %w", record.Topic, record.Partition, record.Offset, err), "poll fetches")
		}

		out = append(out, ConsumedMessage{
			Envelope: envelope,
			Message:  message,
		})
	}

	return out, nil
}

func headersToMap(headers []kgo.RecordHeader) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(headers))
	for _, header := range headers {
		out[header.Key] = string(header.Value)
	}

	return out
}

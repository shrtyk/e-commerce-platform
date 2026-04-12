package kafka

import (
	"context"
	"fmt"
	"maps"
	"strings"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
)

const productCreatedRecordName = "ecommerce.catalog.v1.ProductCreated"

type syncProducer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

type schemaRegistry interface {
	CreateSchema(ctx context.Context, subject string, s sr.Schema) (sr.SubjectSchema, error)
}

type Publisher struct {
	producer *commonkafka.Producer
}

func NewPublisher(client syncProducer, schemaRegistryClient schemaRegistry) (*Publisher, error) {
	if client == nil {
		return nil, fmt.Errorf("kafka producer is nil")
	}

	if schemaRegistryClient == nil {
		return nil, fmt.Errorf("schema registry client is nil")
	}

	serde := commonkafka.NewProtoSerde(schemaRegistryClient, commonkafka.NewDescriptorSchemaProvider())
	producer, err := commonkafka.NewProducer(client, serde, commonkafka.DefaultRetryPolicy())
	if err != nil {
		return nil, fmt.Errorf("create common kafka producer: %w", err)
	}

	return &Publisher{producer: producer}, nil
}

func (p *Publisher) Publish(ctx context.Context, record commonoutbox.Record) error {
	if strings.TrimSpace(record.Topic) == "" {
		return fmt.Errorf("record topic is required")
	}

	if len(record.Payload) == 0 {
		return fmt.Errorf("record payload is required")
	}

	message, metadata, headers, err := p.decodeOutboxRecord(record)
	if err != nil {
		return err
	}

	if err := p.producer.PublishProto(ctx, record.Topic, record.Key, headers, metadata, message); err != nil {
		return fmt.Errorf("publish proto via common producer: %w", err)
	}

	return nil
}

func (p *Publisher) decodeOutboxRecord(record commonoutbox.Record) (proto.Message, commonkafka.EventMetadata, map[string]string, error) {
	if p == nil || p.producer == nil {
		return nil, commonkafka.EventMetadata{}, nil, fmt.Errorf("publisher is nil")
	}

	recordName := strings.TrimSpace(record.Headers[commonkafka.HeaderRecordName])
	if recordName == "" {
		recordName = productCreatedRecordName
	}

	metadata := commonkafka.MetadataFromHeaders(record.Headers)
	if metadata.EventID == "" {
		metadata.EventID = record.EventID
	}
	if metadata.EventName == "" {
		metadata.EventName = record.EventName
	}

	headers := cloneHeaders(record.Headers)

	switch recordName {
	case productCreatedRecordName:
		var message catalogv1.ProductCreated
		if err := proto.Unmarshal(record.Payload, &message); err != nil {
			return nil, commonkafka.EventMetadata{}, nil, fmt.Errorf("unmarshal product created payload: %w", err)
		}

		messageMetadata := message.GetMetadata()
		if messageMetadata != nil {
			metadata = commonkafka.MetadataFromProto(messageMetadata)
		}

		if metadata.EventID == "" {
			metadata.EventID = record.EventID
		}
		if metadata.EventName == "" {
			metadata.EventName = record.EventName
		}

		return &message, metadata, headers, nil
	default:
		return nil, commonkafka.EventMetadata{}, nil, fmt.Errorf("unsupported record name: %s", recordName)
	}
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	clone := make(map[string]string, len(headers))
	maps.Copy(clone, headers)

	return clone
}

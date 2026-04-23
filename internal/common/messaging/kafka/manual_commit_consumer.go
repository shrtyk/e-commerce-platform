package kafka

import (
	"context"
	"fmt"
)

type pollConsumer interface {
	Poll(ctx context.Context) ([]ConsumedMessage, error)
}

type offsetCommitClient interface {
	CommitUncommittedOffsets(ctx context.Context) error
}

type ConsumerWithManualCommit struct {
	consumer pollConsumer
	client   offsetCommitClient
}

func NewConsumerWithManualCommit(consumer pollConsumer, client offsetCommitClient) (*ConsumerWithManualCommit, error) {
	if consumer == nil {
		return nil, fmt.Errorf("poll consumer is nil")
	}

	if client == nil {
		return nil, fmt.Errorf("offset commit client is nil")
	}

	return &ConsumerWithManualCommit{consumer: consumer, client: client}, nil
}

func (c *ConsumerWithManualCommit) Poll(ctx context.Context) ([]ConsumedMessage, error) {
	return c.consumer.Poll(ctx)
}

func (c *ConsumerWithManualCommit) CommitUncommittedOffsets(ctx context.Context) error {
	return c.client.CommitUncommittedOffsets(ctx)
}

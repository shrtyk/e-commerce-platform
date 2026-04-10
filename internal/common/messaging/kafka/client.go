package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

type Client struct {
	inner *kgo.Client
}

func NewClient(opts ...kgo.Opt) (*Client, error) {
	inner, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}

	return &Client{inner: inner}, nil
}

func (c *Client) Close() {
	if c == nil || c.inner == nil {
		return
	}

	c.inner.Close()
}

func (c *Client) ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults {
	return c.inner.ProduceSync(ctx, records...)
}

func (c *Client) PollFetches(ctx context.Context) kgo.Fetches {
	return c.inner.PollFetches(ctx)
}

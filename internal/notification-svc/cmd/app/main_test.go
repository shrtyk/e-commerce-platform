package main

import (
	"context"
	"testing"

	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
)

func TestOrderEventsTopicsIncludesMainAndRetry(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"order.events", "order.events.retry"}, orderEventsTopics("order.events"))
}

type notificationTestSchemaRegistry struct {
	subjects []string
	nextID   int
}

func (r *notificationTestSchemaRegistry) CreateSchema(_ context.Context, subject string, _ sr.Schema) (sr.SubjectSchema, error) {
	r.subjects = append(r.subjects, subject)
	r.nextID++
	return sr.SubjectSchema{ID: r.nextID, Version: 1}, nil
}

type notificationTestSchemaProvider struct{}

func (notificationTestSchemaProvider) SchemaFor(message proto.Message) (commonkafka.SchemaDefinition, error) {
	return commonkafka.SchemaDefinition{Schema: `syntax = "proto3";`, Index: []int{0}}, nil
}

func TestOrderEventSerdeRegistersAllConsumedTypes(t *testing.T) {
	t.Parallel()

	registry := &notificationTestSchemaRegistry{}
	serde := commonkafka.NewProtoSerde(registry, notificationTestSchemaProvider{})

	err := serde.RegisterTypes(
		context.Background(),
		"order.events",
		&orderv1.OrderCreated{},
		&orderv1.OrderConfirmed{},
		&orderv1.OrderCancelled{},
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		commonkafka.TopicRecordNameSubject("order.events", "ecommerce.order.v1.OrderCreated"),
		commonkafka.TopicRecordNameSubject("order.events", "ecommerce.order.v1.OrderConfirmed"),
		commonkafka.TopicRecordNameSubject("order.events", "ecommerce.order.v1.OrderCancelled"),
	}, registry.subjects)
}

func TestOrderEventSerdeRegistersRetryTopicTypes(t *testing.T) {
	t.Parallel()

	registry := &notificationTestSchemaRegistry{}
	serde := commonkafka.NewProtoSerde(registry, notificationTestSchemaProvider{})

	err := serde.RegisterTypes(
		context.Background(),
		"order.events.retry",
		&orderv1.OrderCreated{},
		&orderv1.OrderConfirmed{},
		&orderv1.OrderCancelled{},
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		commonkafka.TopicRecordNameSubject("order.events.retry", "ecommerce.order.v1.OrderCreated"),
		commonkafka.TopicRecordNameSubject("order.events.retry", "ecommerce.order.v1.OrderConfirmed"),
		commonkafka.TopicRecordNameSubject("order.events.retry", "ecommerce.order.v1.OrderCancelled"),
	}, registry.subjects)
}

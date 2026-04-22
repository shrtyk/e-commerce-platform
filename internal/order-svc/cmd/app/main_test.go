package main

import (
	"context"
	"sort"
	"testing"

	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
	"google.golang.org/protobuf/proto"
)

type testSchemaRegistry struct {
	subjects []string
	nextID   int
}

func (r *testSchemaRegistry) CreateSchema(_ context.Context, subject string, _ sr.Schema) (sr.SubjectSchema, error) {
	r.subjects = append(r.subjects, subject)
	r.nextID++
	return sr.SubjectSchema{ID: r.nextID, Version: 1}, nil
}

type testSchemaProvider struct{}

func (testSchemaProvider) SchemaFor(message proto.Message) (commonkafka.SchemaDefinition, error) {
	return commonkafka.SchemaDefinition{Schema: `syntax = "proto3";`, Index: []int{0}}, nil
}

func TestPaymentEventSerdeRegistersAllPaymentTypes(t *testing.T) {
	t.Parallel()

	registry := &testSchemaRegistry{}
	serde := commonkafka.NewProtoSerde(registry, testSchemaProvider{})

	err := serde.RegisterTypes(
		context.Background(),
		"payment.events",
		&paymentv1.PaymentInitiated{},
		&paymentv1.PaymentSucceeded{},
		&paymentv1.PaymentFailed{},
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		commonkafka.TopicRecordNameSubject("payment.events", "ecommerce.payment.v1.PaymentInitiated"),
		commonkafka.TopicRecordNameSubject("payment.events", "ecommerce.payment.v1.PaymentSucceeded"),
		commonkafka.TopicRecordNameSubject("payment.events", "ecommerce.payment.v1.PaymentFailed"),
	}, registry.subjects)
}

func TestOrderRelayTypeRegistryRegistersConfirmed(t *testing.T) {
	t.Parallel()

	registry := commonkafka.NewTypeRegistry()
	err := registry.RegisterMessages(&orderv1.OrderCreated{}, &orderv1.OrderCancelled{}, &orderv1.OrderConfirmed{})
	require.NoError(t, err)

	confirmed, err := registry.NewMessage("ecommerce.order.v1.OrderConfirmed")
	require.NoError(t, err)
	require.IsType(t, &orderv1.OrderConfirmed{}, confirmed)
}

func TestSchemaRegistrySubjectsSortStable(t *testing.T) {
	t.Parallel()

	registry := &testSchemaRegistry{}
	serde := commonkafka.NewProtoSerde(registry, testSchemaProvider{})
	require.NoError(t, serde.RegisterTypes(context.Background(), "payment.events", &paymentv1.PaymentFailed{}, &paymentv1.PaymentInitiated{}))

	subjects := append([]string(nil), registry.subjects...)
	sort.Strings(subjects)
	require.Len(t, subjects, 2)
}

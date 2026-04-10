package kafka

import (
	"context"
	"fmt"
	"testing"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
	"github.com/twmb/franz-go/pkg/sr/srfake"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type testSchemaProvider struct {
	schemas map[protoreflect.FullName]SchemaDefinition
}

func (p testSchemaProvider) SchemaFor(message proto.Message) (SchemaDefinition, error) {
	name := message.ProtoReflect().Descriptor().FullName()
	schema, ok := p.schemas[name]
	if !ok {
		return SchemaDefinition{}, fmt.Errorf("schema for %s not found", name)
	}

	return schema, nil
}

func TestProtoSerdeRoundTripTopicRecordNameStrategy(t *testing.T) {
	registry := srfake.New()
	t.Cleanup(registry.Close)

	registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
	require.NoError(t, err)

	provider := testSchemaProvider{
		schemas: map[protoreflect.FullName]SchemaDefinition{
			(&catalogv1.ProductCreated{}).ProtoReflect().Descriptor().FullName(): {
				Schema: `syntax = "proto3"; message ProductCreated { string product_id = 1; }`,
				Index:  []int{0},
			},
			(&orderv1.OrderCreated{}).ProtoReflect().Descriptor().FullName(): {
				Schema: `syntax = "proto3"; message OrderCreated { string order_id = 1; }`,
				Index:  []int{0},
			},
		},
	}

	serde := NewProtoSerde(registryClient, provider)
	ctx := context.Background()
	topic := "catalog.product.events"

	productPayload := &catalogv1.ProductCreated{ProductId: "product-1", Name: "Sneakers"}
	orderPayload := &orderv1.OrderCreated{OrderId: "order-1", UserId: "user-1", Currency: "USD"}

	encodedProduct, productRecordName, err := serde.Encode(ctx, topic, productPayload)
	require.NoError(t, err)
	require.Equal(t, "ecommerce.catalog.v1.ProductCreated", productRecordName)

	encodedOrder, orderRecordName, err := serde.Encode(ctx, topic, orderPayload)
	require.NoError(t, err)
	require.Equal(t, "ecommerce.order.v1.OrderCreated", orderRecordName)

	subjects, err := registryClient.Subjects(ctx)
	require.NoError(t, err)
	require.Contains(t, subjects, TopicRecordNameSubject(topic, productRecordName))
	require.Contains(t, subjects, TopicRecordNameSubject(topic, orderRecordName))

	decodedProduct, err := serde.Decode(encodedProduct)
	require.NoError(t, err)
	decodedProductMessage, ok := decodedProduct.(*catalogv1.ProductCreated)
	require.True(t, ok)
	require.Equal(t, "product-1", decodedProductMessage.GetProductId())

	decodedOrder, err := serde.Decode(encodedOrder)
	require.NoError(t, err)
	decodedOrderMessage, ok := decodedOrder.(*orderv1.OrderCreated)
	require.True(t, ok)
	require.Equal(t, "order-1", decodedOrderMessage.GetOrderId())
}

func TestProtoSerdeRegistersSameTypePerTopic(t *testing.T) {
	registry := srfake.New()
	t.Cleanup(registry.Close)

	registryClient, err := sr.NewClient(sr.URLs(registry.URL()))
	require.NoError(t, err)

	serde := NewProtoSerde(registryClient, staticSchemaProvider{})
	ctx := context.Background()

	message := &catalogv1.ProductCreated{ProductId: "product-1", Name: "Sneakers"}

	_, recordName, err := serde.Encode(ctx, "catalog.product.events", message)
	require.NoError(t, err)

	_, _, err = serde.Encode(ctx, "catalog.product.retry.events", message)
	require.NoError(t, err)

	firstSubject := TopicRecordNameSubject("catalog.product.events", recordName)
	secondSubject := TopicRecordNameSubject("catalog.product.retry.events", recordName)

	subjects, err := registryClient.Subjects(ctx)
	require.NoError(t, err)
	require.Contains(t, subjects, firstSubject)
	require.Contains(t, subjects, secondSubject)

	firstVersions, err := registryClient.SubjectVersions(ctx, firstSubject)
	require.NoError(t, err)
	require.Len(t, firstVersions, 1)

	secondVersions, err := registryClient.SubjectVersions(ctx, secondSubject)
	require.NoError(t, err)
	require.Len(t, secondVersions, 1)
}

func TestProtoSerdeEncodeNilReceiver(t *testing.T) {
	var serde *ProtoSerde

	_, _, err := serde.Encode(context.Background(), "catalog.product.events", &catalogv1.ProductCreated{})
	require.Error(t, err)
	require.True(t, IsNonRetriable(err))
	require.ErrorContains(t, err, "proto serde is nil")
}

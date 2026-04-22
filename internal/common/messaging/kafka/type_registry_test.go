package kafka

import (
	"testing"

	notificationv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/notification/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
)

func TestTypeRegistryNewMessage(t *testing.T) {
	const recordName = "ecommerce.catalog.v1.ProductCreated"

	tests := []struct {
		name        string
		setup       func(registry *TypeRegistry)
		recordName  string
		errContains string
		assertFn    func(t *testing.T, msg proto.Message)
	}{
		{
			name: "returns new message from registered factory",
			setup: func(registry *TypeRegistry) {
				err := registry.RegisterMessages(&catalogv1.ProductCreated{})
				require.NoError(t, err)
			},
			recordName: recordName,
			assertFn: func(t *testing.T, msg proto.Message) {
				_, ok := msg.(*catalogv1.ProductCreated)
				require.True(t, ok)
			},
		},
		{
			name:        "unsupported record name",
			recordName:  "ecommerce.catalog.v1.Unknown",
			errContains: "unsupported record name",
		},
		{
			name:        "empty record name",
			recordName:  "",
			errContains: "record name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewTypeRegistry()
			if tt.setup != nil {
				tt.setup(registry)
			}

			msg, err := registry.NewMessage(tt.recordName)
			if tt.errContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, msg)
			if tt.assertFn != nil {
				tt.assertFn(t, msg)
			}
		})
	}

	t.Run("returns fresh instance for each call", func(t *testing.T) {
		registry := NewTypeRegistry()
		err := registry.RegisterMessages(&catalogv1.ProductCreated{})
		require.NoError(t, err)

		first, err := registry.NewMessage(recordName)
		require.NoError(t, err)

		second, err := registry.NewMessage(recordName)
		require.NoError(t, err)

		require.NotSame(t, first, second)
	})
}

func TestTypeRegistryRegisterNilReceiver(t *testing.T) {
	var registry *TypeRegistry

	err := registry.RegisterMessages(&catalogv1.ProductCreated{})
	require.Error(t, err)
	require.ErrorContains(t, err, "type registry is nil")
}

func TestTypeRegistryNewMessageNilReceiver(t *testing.T) {
	var registry *TypeRegistry

	msg, err := registry.NewMessage("ecommerce.catalog.v1.ProductCreated")
	require.Error(t, err)
	require.ErrorContains(t, err, "type registry is nil")
	require.Nil(t, msg)
}

func TestTypeRegistryRegisterMessagesRegistersAllSamples(t *testing.T) {
	registry := NewTypeRegistry()

	err := registry.RegisterMessages(
		&catalogv1.ProductCreated{},
		&notificationv1.NotificationSent{},
	)
	require.NoError(t, err)

	product, err := registry.NewMessage("ecommerce.catalog.v1.ProductCreated")
	require.NoError(t, err)
	require.IsType(t, &catalogv1.ProductCreated{}, product)

	notification, err := registry.NewMessage("ecommerce.notification.v1.NotificationSent")
	require.NoError(t, err)
	require.IsType(t, &notificationv1.NotificationSent{}, notification)
}

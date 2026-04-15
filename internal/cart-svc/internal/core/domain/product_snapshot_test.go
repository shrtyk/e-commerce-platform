package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewProductSnapshotValidatesInput(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	productID := uuid.New()

	tests := []struct {
		name      string
		sku       string
		nameValue string
		unitPrice int64
		currency  string
		err       error
	}{
		{
			name:      "sku",
			sku:       "",
			nameValue: "Item",
			unitPrice: 100,
			currency:  "USD",
			err:       ErrInvalidSKU,
		},
		{
			name:      "name",
			sku:       "SKU-1",
			nameValue: "",
			unitPrice: 100,
			currency:  "USD",
			err:       ErrInvalidName,
		},
		{
			name:      "unit price",
			sku:       "SKU-1",
			nameValue: "Item",
			unitPrice: -1,
			currency:  "USD",
			err:       ErrNegativeAmount,
		},
		{
			name:      "currency",
			sku:       "SKU-1",
			nameValue: "Item",
			unitPrice: 100,
			currency:  "   ",
			err:       ErrInvalidCurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			snapshot, err := NewProductSnapshot(tt.sku, &productID, tt.nameValue, tt.unitPrice, tt.currency, now, now)

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.err))
			require.Equal(t, ProductSnapshot{}, snapshot)
		})
	}
}

func TestNewProductSnapshotNormalizesValues(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	productID := uuid.New()

	snapshot, err := NewProductSnapshot(" SKU-1 ", &productID, " Item A ", 1200, " usd ", now, now)

	require.NoError(t, err)
	require.Equal(t, "SKU-1", snapshot.SKU)
	require.Equal(t, "Item A", snapshot.Name)
	require.Equal(t, "USD", snapshot.Currency)
	require.Equal(t, int64(1200), snapshot.UnitPrice)
	require.Equal(t, &productID, snapshot.ProductID)
}

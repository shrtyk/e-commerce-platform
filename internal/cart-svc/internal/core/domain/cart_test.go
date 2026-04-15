package domain

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewCartItemValidatesInput(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	tests := []struct {
		name      string
		sku       string
		itemName  string
		quantity  int64
		unitPrice int64
		currency  string
		err       error
	}{
		{
			name:      "sku",
			sku:       "",
			itemName:  "Item",
			quantity:  1,
			unitPrice: 100,
			currency:  "USD",
			err:       ErrInvalidSKU,
		},
		{
			name:      "name",
			sku:       "SKU-1",
			itemName:  "",
			quantity:  1,
			unitPrice: 100,
			currency:  "USD",
			err:       ErrInvalidName,
		},
		{
			name:      "quantity",
			sku:       "SKU-1",
			itemName:  "Item",
			quantity:  0,
			unitPrice: 100,
			currency:  "USD",
			err:       ErrInvalidQuantity,
		},
		{
			name:      "unit price",
			sku:       "SKU-1",
			itemName:  "Item",
			quantity:  1,
			unitPrice: -1,
			currency:  "USD",
			err:       ErrNegativeAmount,
		},
		{
			name:      "currency",
			sku:       "SKU-1",
			itemName:  "Item",
			quantity:  1,
			unitPrice: 100,
			currency:  "",
			err:       ErrInvalidCurrency,
		},
		{
			name:      "line total overflow",
			sku:       "SKU-1",
			itemName:  "Item",
			quantity:  2,
			unitPrice: math.MaxInt64,
			currency:  "USD",
			err:       ErrAmountOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			item, err := NewCartItem(tt.sku, tt.itemName, tt.quantity, tt.unitPrice, tt.currency, now, now)

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.err))
			require.Equal(t, CartItem{}, item)
		})
	}
}

func TestNewCartRecalculatesTotalsAndNormalizesItems(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	itemA, err := NewCartItem(" SKU-1 ", " Item A ", 2, 1200, "USD", now, now)
	require.NoError(t, err)

	itemB, err := NewCartItem("SKU-2", "Item B", 1, 3500, "USD", now, now)
	require.NoError(t, err)

	cart, err := NewCart(uuid.New(), uuid.New(), CartStatusActive, " USD ", []CartItem{itemA, itemB}, now, now)

	require.NoError(t, err)
	require.Equal(t, "USD", cart.Currency)
	require.Equal(t, int64(5900), cart.TotalAmount)
	require.Equal(t, int64(2400), cart.Items[0].LineTotal)
	require.Equal(t, "SKU-1", cart.Items[0].SKU)
	require.Equal(t, "Item A", cart.Items[0].Name)
}

func TestNewCartRejectsCurrencyMismatch(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	item, err := NewCartItem("SKU-1", "Item", 1, 100, "USD", now, now)
	require.NoError(t, err)

	cart, err := NewCart(uuid.New(), uuid.New(), CartStatusActive, "EUR", []CartItem{item}, now, now)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCurrencyMismatch))
	require.Equal(t, Cart{}, cart)
}

func TestNewCartValidatesStatusAndCurrency(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	tests := []struct {
		name     string
		status   CartStatus
		currency string
		err      error
	}{
		{
			name:     "status",
			status:   CartStatus("paused"),
			currency: "USD",
			err:      ErrInvalidCartStatus,
		},
		{
			name:     "currency",
			status:   CartStatusActive,
			currency: "   ",
			err:      ErrInvalidCurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cart, err := NewCart(uuid.New(), uuid.New(), tt.status, tt.currency, nil, now, now)

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.err))
			require.Equal(t, Cart{}, cart)
		})
	}
}

func TestCartRecalculateTotals(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	item, err := NewCartItem("SKU-1", "Item", 1, 100, "USD", now, now)
	require.NoError(t, err)

	cart := Cart{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		Status:   CartStatusActive,
		Currency: "USD",
		Items:    []CartItem{item},
	}

	cart.Items[0].Quantity = 3
	cart.Items[0].LineTotal = 0

	recalculated, err := cart.RecalculateTotals()

	require.NoError(t, err)
	require.Equal(t, int64(300), recalculated.TotalAmount)
	require.Equal(t, int64(300), recalculated.Items[0].LineTotal)
}

func TestCartRecalculateTotalsOverflow(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	first, err := NewCartItem("SKU-1", "Item 1", 1, math.MaxInt64, "USD", now, now)
	require.NoError(t, err)

	second, err := NewCartItem("SKU-2", "Item 2", 1, 1, "USD", now, now)
	require.NoError(t, err)

	cart := Cart{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		Status:   CartStatusActive,
		Currency: "USD",
		Items:    []CartItem{first, second},
	}

	recalculated, err := cart.RecalculateTotals()

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrAmountOverflow))
	require.Equal(t, Cart{}, recalculated)
}

func TestCartRecalculateTotalsNormalizesAndValidatesCartCurrency(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	item, err := NewCartItem("SKU-1", "Item", 2, 100, "USD", now, now)
	require.NoError(t, err)

	tests := []struct {
		name     string
		cart     Cart
		err      error
		currency string
		total    int64
	}{
		{
			name: "invalid currency empty cart",
			cart: Cart{
				ID:       uuid.New(),
				UserID:   uuid.New(),
				Status:   CartStatusActive,
				Currency: "   ",
				Items:    nil,
			},
			err: ErrInvalidCurrency,
		},
		{
			name: "unnormalized currency empty cart",
			cart: Cart{
				ID:       uuid.New(),
				UserID:   uuid.New(),
				Status:   CartStatusActive,
				Currency: " usd ",
				Items:    nil,
			},
			currency: "USD",
			total:    0,
		},
		{
			name: "unnormalized currency with items",
			cart: Cart{
				ID:       uuid.New(),
				UserID:   uuid.New(),
				Status:   CartStatusActive,
				Currency: " usd ",
				Items:    []CartItem{item},
			},
			currency: "USD",
			total:    200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recalculated, recalcErr := tt.cart.RecalculateTotals()

			if tt.err != nil {
				require.Error(t, recalcErr)
				require.True(t, errors.Is(recalcErr, tt.err))
				require.Equal(t, Cart{}, recalculated)
				return
			}

			require.NoError(t, recalcErr)
			require.Equal(t, tt.currency, recalculated.Currency)
			require.Equal(t, tt.total, recalculated.TotalAmount)
		})
	}
}

package domain

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CartStatus string

const (
	CartStatusUnknown    CartStatus = ""
	CartStatusActive     CartStatus = "active"
	CartStatusCheckedOut CartStatus = "checked_out"
	CartStatusExpired    CartStatus = "expired"
)

type Cart struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Status      CartStatus
	Currency    string
	Items       []CartItem
	TotalAmount int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CartItem struct {
	SKU       string
	Name      string
	Quantity  int64
	UnitPrice int64
	Currency  string
	LineTotal int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewCart(id uuid.UUID, userID uuid.UUID, status CartStatus, currency string, items []CartItem, createdAt time.Time, updatedAt time.Time) (Cart, error) {
	validatedStatus, err := normalizeCartStatus(status)
	if err != nil {
		return Cart{}, err
	}

	normalizedCurrency, err := normalizeCurrency(currency)
	if err != nil {
		return Cart{}, err
	}

	validatedItems, total, err := normalizeCartItems(items, normalizedCurrency)
	if err != nil {
		return Cart{}, err
	}

	return Cart{
		ID:          id,
		UserID:      userID,
		Status:      validatedStatus,
		Currency:    normalizedCurrency,
		Items:       validatedItems,
		TotalAmount: total,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func NewCartItem(sku string, name string, quantity int64, unitPrice int64, currency string, createdAt time.Time, updatedAt time.Time) (CartItem, error) {
	normalizedSKU, err := normalizeSKU(sku)
	if err != nil {
		return CartItem{}, err
	}

	normalizedName, err := normalizeName(name)
	if err != nil {
		return CartItem{}, err
	}

	validatedQuantity, err := validateQuantity(quantity)
	if err != nil {
		return CartItem{}, err
	}

	if unitPrice < 0 {
		return CartItem{}, ErrNegativeAmount
	}

	normalizedCurrency, err := normalizeCurrency(currency)
	if err != nil {
		return CartItem{}, err
	}

	lineTotal, err := multiplyAmountByQuantity(unitPrice, validatedQuantity)
	if err != nil {
		return CartItem{}, err
	}

	return CartItem{
		SKU:       normalizedSKU,
		Name:      normalizedName,
		Quantity:  validatedQuantity,
		UnitPrice: unitPrice,
		Currency:  normalizedCurrency,
		LineTotal: lineTotal,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (c Cart) RecalculateTotals() (Cart, error) {
	normalizedCurrency, err := normalizeCurrency(c.Currency)
	if err != nil {
		return Cart{}, err
	}

	validatedItems, total, err := normalizeCartItems(c.Items, normalizedCurrency)
	if err != nil {
		return Cart{}, err
	}

	c.Currency = normalizedCurrency
	c.Items = validatedItems
	c.TotalAmount = total

	return c, nil
}

func normalizeCartStatus(status CartStatus) (CartStatus, error) {
	switch status {
	case CartStatusActive, CartStatusCheckedOut, CartStatusExpired:
		return status, nil
	default:
		return CartStatusUnknown, ErrInvalidCartStatus
	}
}

func normalizeCartItems(items []CartItem, cartCurrency string) ([]CartItem, int64, error) {
	validatedItems := make([]CartItem, len(items))
	var total int64

	for i := range items {
		item, err := normalizeExistingItem(items[i], cartCurrency)
		if err != nil {
			return nil, 0, fmt.Errorf("validate cart item %d: %w", i, err)
		}

		nextTotal, err := addAmounts(total, item.LineTotal)
		if err != nil {
			return nil, 0, fmt.Errorf("sum cart totals: %w", err)
		}

		total = nextTotal
		validatedItems[i] = item
	}

	return validatedItems, total, nil
}

func normalizeExistingItem(item CartItem, cartCurrency string) (CartItem, error) {
	normalizedSKU, err := normalizeSKU(item.SKU)
	if err != nil {
		return CartItem{}, err
	}

	normalizedName, err := normalizeName(item.Name)
	if err != nil {
		return CartItem{}, err
	}

	validatedQuantity, err := validateQuantity(item.Quantity)
	if err != nil {
		return CartItem{}, err
	}

	if item.UnitPrice < 0 {
		return CartItem{}, ErrNegativeAmount
	}

	normalizedCurrency, err := normalizeCurrency(item.Currency)
	if err != nil {
		return CartItem{}, err
	}

	if normalizedCurrency != cartCurrency {
		return CartItem{}, ErrCurrencyMismatch
	}

	lineTotal, err := multiplyAmountByQuantity(item.UnitPrice, validatedQuantity)
	if err != nil {
		return CartItem{}, err
	}

	return CartItem{
		SKU:       normalizedSKU,
		Name:      normalizedName,
		Quantity:  validatedQuantity,
		UnitPrice: item.UnitPrice,
		Currency:  normalizedCurrency,
		LineTotal: lineTotal,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}, nil
}

func normalizeSKU(sku string) (string, error) {
	normalized := strings.TrimSpace(sku)
	if normalized == "" {
		return "", ErrInvalidSKU
	}

	return normalized, nil
}

func normalizeCurrency(currency string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(currency))
	if normalized == "" {
		return "", ErrInvalidCurrency
	}

	return normalized, nil
}

func normalizeName(name string) (string, error) {
	normalized := strings.TrimSpace(name)
	if normalized == "" {
		return "", ErrInvalidName
	}

	return normalized, nil
}

func validateQuantity(quantity int64) (int64, error) {
	if quantity <= 0 {
		return 0, ErrInvalidQuantity
	}

	return quantity, nil
}

func multiplyAmountByQuantity(amount int64, quantity int64) (int64, error) {
	if amount < 0 {
		return 0, ErrNegativeAmount
	}

	if quantity <= 0 {
		return 0, ErrInvalidQuantity
	}

	if amount != 0 && quantity > math.MaxInt64/amount {
		return 0, ErrAmountOverflow
	}

	return amount * quantity, nil
}

func addAmounts(left int64, right int64) (int64, error) {
	if right > math.MaxInt64-left {
		return 0, ErrAmountOverflow
	}

	return left + right, nil
}

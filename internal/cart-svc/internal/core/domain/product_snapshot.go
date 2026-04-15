package domain

import (
	"time"

	"github.com/google/uuid"
)

type ProductSnapshot struct {
	SKU       string
	ProductID *uuid.UUID
	Name      string
	UnitPrice int64
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewProductSnapshot(sku string, productID *uuid.UUID, name string, unitPrice int64, currency string, createdAt time.Time, updatedAt time.Time) (ProductSnapshot, error) {
	normalizedSKU, err := normalizeSKU(sku)
	if err != nil {
		return ProductSnapshot{}, err
	}

	normalizedName, err := normalizeName(name)
	if err != nil {
		return ProductSnapshot{}, err
	}

	if unitPrice < 0 {
		return ProductSnapshot{}, ErrNegativeAmount
	}

	normalizedCurrency, err := normalizeCurrency(currency)
	if err != nil {
		return ProductSnapshot{}, err
	}

	return ProductSnapshot{
		SKU:       normalizedSKU,
		ProductID: productID,
		Name:      normalizedName,
		UnitPrice: unitPrice,
		Currency:  normalizedCurrency,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

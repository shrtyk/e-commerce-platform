package domain

import "errors"

var (
	ErrInvalidCartStatus = errors.New("cart invalid status")
	ErrInvalidSKU        = errors.New("cart invalid sku")
	ErrInvalidQuantity   = errors.New("cart invalid quantity")
	ErrInvalidCurrency   = errors.New("cart invalid currency")
	ErrInvalidName       = errors.New("cart invalid name")
	ErrNegativeAmount    = errors.New("cart negative amount")
	ErrAmountOverflow    = errors.New("cart amount overflow")
	ErrCurrencyMismatch  = errors.New("cart currency mismatch")
)

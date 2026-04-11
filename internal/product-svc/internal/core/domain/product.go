package domain

import (
	"time"

	"github.com/google/uuid"
)

type ProductStatus string

const (
	ProductStatusUnknown   ProductStatus = ""
	ProductStatusDraft     ProductStatus = "draft"
	ProductStatusPublished ProductStatus = "published"
	ProductStatusArchived  ProductStatus = "archived"
)

type Product struct {
	ID               uuid.UUID
	SKU              string
	Name             string
	Description      string
	Price            int64
	CurrencyID       uuid.UUID
	Currency         string
	CurrencyName     string
	CurrencyDecimals int32
	CategoryID       *uuid.UUID
	Status           ProductStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

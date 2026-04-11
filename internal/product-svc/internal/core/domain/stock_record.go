package domain

import (
	"time"

	"github.com/google/uuid"
)

type StockRecordStatus string

const (
	StockRecordStatusUnknown    StockRecordStatus = ""
	StockRecordStatusInStock    StockRecordStatus = "in_stock"
	StockRecordStatusOutOfStock StockRecordStatus = "out_of_stock"
)

type StockRecord struct {
	StockRecordID uuid.UUID
	ProductID     uuid.UUID
	Quantity      int32
	Reserved      int32
	Available     int32
	Status        StockRecordStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

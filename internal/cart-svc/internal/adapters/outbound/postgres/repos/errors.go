package repos

import "errors"

var (
	ErrCartNotFound            = errors.New("cart not found")
	ErrCartAlreadyExists       = errors.New("cart already exists")
	ErrCartItemNotFound        = errors.New("cart item not found")
	ErrCartItemAlreadyExists   = errors.New("cart item already exists")
	ErrProductSnapshotNotFound = errors.New("product snapshot not found")
	ErrProductSnapshotConflict = errors.New("product snapshot conflict")
)

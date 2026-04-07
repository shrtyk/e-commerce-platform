package tx

import (
	"context"
	"errors"
)

var (
	ErrTxCommit   = errors.New("tx commit failed")
	ErrTxRollback = errors.New("tx rollback failed")
)

type UnitOfWork[T any] interface {
	Repos() T
}

type Provider[T any] interface {
	WithTransaction(ctx context.Context, fn func(uow UnitOfWork[T]) error) error
}

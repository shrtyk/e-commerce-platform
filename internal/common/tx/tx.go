package tx

import (
	"context"
	"database/sql"
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
	WithTransaction(ctx context.Context, txOpts *sql.TxOptions, fn func(uow UnitOfWork[T]) error) error
}

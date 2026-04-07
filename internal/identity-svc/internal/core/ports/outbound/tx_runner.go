package outbound

import (
	"context"
	"errors"
)

var (
	ErrTxCommit   = errors.New("identity tx commit failed")
	ErrTxRollback = errors.New("identity tx rollback failed")
)

//mockery:generate: true
type TxScope interface {
	UserRepository() UserRepository
	SessionRepository() SessionRepository
}

//mockery:generate: true
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, tx TxScope) error) error
}

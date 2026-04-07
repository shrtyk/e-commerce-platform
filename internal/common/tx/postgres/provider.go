package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
)

type unitOfWork[T any] struct {
	repos T
}

func (u *unitOfWork[T]) Repos() T {
	return u.repos
}

type Provider[T any] struct {
	db         *sql.DB
	buildRepos func(tx *sql.Tx) T
}

func NewProvider[T any](db *sql.DB, buildRepos func(*sql.Tx) T) *Provider[T] {
	return &Provider[T]{db: db, buildRepos: buildRepos}
}

func (p *Provider[T]) WithTransaction(ctx context.Context, fn func(tx.UnitOfWork[T]) error) error {
	sqlTx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	uow := &unitOfWork[T]{repos: p.buildRepos(sqlTx)}

	defer func() {
		if r := recover(); r != nil {
			if rbErr := sqlTx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				panic(errors.Join(
					fmt.Errorf("panic recovered: %v", r),
					fmt.Errorf("%w: %w", tx.ErrTxRollback, rbErr),
				))
			}
			panic(r)
		}

		if err != nil {
			if rbErr := sqlTx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(
					fmt.Errorf("within tx callback: %w", err),
					fmt.Errorf("%w: %w", tx.ErrTxRollback, rbErr),
				)
			}
			return
		}

		if commitErr := sqlTx.Commit(); commitErr != nil {
			err = fmt.Errorf("%w: %w", tx.ErrTxCommit, commitErr)
		}
	}()

	err = fn(uow)
	return err
}

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/repos"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

type TxRunner struct {
	db *sql.DB
}

func NewTxRunner(db *sql.DB) *TxRunner {
	return &TxRunner{db: db}
}

func (r *TxRunner) WithinTx(ctx context.Context, fn func(ctx context.Context, tx outbound.TxScope) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	scope := txScope{
		userRepository:    repos.NewUserRepositoryFromQuerier(sqlc.New(tx)),
		sessionRepository: repos.NewSessionRepositoryFromQuerier(sqlc.New(tx)),
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		rbErr := tx.Rollback()
		if rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			panic(fmt.Errorf("%w: %w", outbound.ErrTxRollback, rbErr))
		}

		panic(recovered)
	}()

	err = fn(ctx, scope)
	if err != nil {
		rbErr := tx.Rollback()
		if rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(
				fmt.Errorf("within tx callback: %w", err),
				fmt.Errorf("%w: %w", outbound.ErrTxRollback, rbErr),
			)
		}

		return errors.Join(
			fmt.Errorf("within tx callback: %w", err),
			outbound.ErrTxRollback,
		)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("%w: %w", outbound.ErrTxCommit, err)
	}

	return nil
}

type txScope struct {
	userRepository    outbound.UserRepository
	sessionRepository outbound.SessionRepository
}

func (s txScope) UserRepository() outbound.UserRepository {
	return s.userRepository
}

func (s txScope) SessionRepository() outbound.SessionRepository {
	return s.sessionRepository
}

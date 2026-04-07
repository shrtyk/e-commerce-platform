package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

func TestTxRunnerWithinTxCommitOnSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	runner := NewTxRunner(db)

	mock.ExpectBegin()
	mock.ExpectCommit()

	err = runner.WithinTx(context.Background(), func(_ context.Context, tx outbound.TxScope) error {
		require.NotNil(t, tx.UserRepository())
		require.NotNil(t, tx.SessionRepository())
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTxRunnerWithinTxRollbackOnCallbackError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	runner := NewTxRunner(db)
	callbackErr := errors.New("callback error")

	mock.ExpectBegin()
	mock.ExpectRollback()

	err = runner.WithinTx(context.Background(), func(_ context.Context, _ outbound.TxScope) error {
		return callbackErr
	})
	require.ErrorIs(t, err, outbound.ErrTxRollback)
	require.ErrorIs(t, err, callbackErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTxRunnerWithinTxReturnsCommitSentinel(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	runner := NewTxRunner(db)
	commitErr := errors.New("commit failed")

	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(commitErr)

	err = runner.WithinTx(context.Background(), func(_ context.Context, _ outbound.TxScope) error {
		return nil
	})
	require.ErrorIs(t, err, outbound.ErrTxCommit)
	require.ErrorIs(t, err, commitErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTxRunnerWithinTxReturnsRollbackSentinel(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	runner := NewTxRunner(db)
	callbackErr := errors.New("callback failed")
	rollbackErr := errors.New("rollback failed")

	mock.ExpectBegin()
	mock.ExpectRollback().WillReturnError(rollbackErr)

	err = runner.WithinTx(context.Background(), func(_ context.Context, _ outbound.TxScope) error {
		return callbackErr
	})
	require.ErrorIs(t, err, outbound.ErrTxRollback)
	require.ErrorIs(t, err, callbackErr)
	require.ErrorIs(t, err, rollbackErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTxRunnerWithinTxRollbackOnPanic(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	runner := NewTxRunner(db)

	mock.ExpectBegin()
	mock.ExpectRollback()

	require.PanicsWithValue(t, "panic callback", func() {
		_ = runner.WithinTx(context.Background(), func(_ context.Context, _ outbound.TxScope) error {
			panic("panic callback")
		})
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

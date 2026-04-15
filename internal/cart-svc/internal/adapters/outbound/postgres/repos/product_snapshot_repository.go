package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/sqlc"
)

type ProductSnapshotRepository struct {
	queries sqlc.Querier
}

func NewProductSnapshotRepository(db *sql.DB) *ProductSnapshotRepository {
	return NewProductSnapshotRepositoryFromQuerier(sqlc.New(db))
}

func NewProductSnapshotRepositoryFromQuerier(queries sqlc.Querier) *ProductSnapshotRepository {
	return &ProductSnapshotRepository{queries: queries}
}

func NewProductSnapshotRepositoryFromTx(tx *sql.Tx) *ProductSnapshotRepository {
	return NewProductSnapshotRepositoryFromQuerier(sqlc.New(tx))
}

func (r *ProductSnapshotRepository) GetBySKU(ctx context.Context, sku string) (sqlc.ProductSnapshot, error) {
	snapshot, err := r.queries.GetProductSnapshotBySKU(ctx, sku)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.ProductSnapshot{}, ErrProductSnapshotNotFound
		}

		return sqlc.ProductSnapshot{}, fmt.Errorf("get product snapshot by sku %q: %w", sku, err)
	}

	return snapshot, nil
}

func (r *ProductSnapshotRepository) Upsert(ctx context.Context, params sqlc.UpsertProductSnapshotParams) (sqlc.ProductSnapshot, error) {
	snapshot, err := r.queries.UpsertProductSnapshot(ctx, params)
	if err != nil {
		mappedErr := mapProductSnapshotWriteErr(err)
		return sqlc.ProductSnapshot{}, fmt.Errorf("upsert product snapshot: %w", mappedErr)
	}

	return snapshot, nil
}

func mapProductSnapshotWriteErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok && pgErr.Code == "23505" {
		return ErrProductSnapshotConflict
	}

	return err
}

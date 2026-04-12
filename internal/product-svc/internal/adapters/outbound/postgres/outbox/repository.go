package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/sqlc"
)

type querier interface {
	AppendOutboxRecord(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error)
	ClaimPendingOutboxRecords(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	MarkOutboxRecordPublished(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error)
	MarkOutboxRecordFailed(ctx context.Context, arg sqlc.MarkOutboxRecordFailedParams) (int64, error)
}

type Repository struct {
	queries querier
}

const claimLockTTL = 30 * time.Second

func NewRepository(db *sql.DB) *Repository {
	return NewRepositoryFromQuerier(sqlc.New(db))
}

func NewRepositoryFromTx(tx *sql.Tx) *Repository {
	return NewRepositoryFromQuerier(sqlc.New(tx))
}

func NewRepositoryFromQuerier(queries querier) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) Append(ctx context.Context, record commonoutbox.Record) (commonoutbox.Record, error) {
	if err := record.ValidateForAppend(); err != nil {
		return commonoutbox.Record{}, err
	}

	headersRaw, err := json.Marshal(record.Headers)
	if err != nil {
		return commonoutbox.Record{}, fmt.Errorf("marshal outbox headers: %w", err)
	}

	created, err := r.queries.AppendOutboxRecord(ctx, sqlc.AppendOutboxRecordParams{
		EventID:       record.EventID,
		EventName:     record.EventName,
		AggregateType: record.AggregateType,
		AggregateID:   record.AggregateID,
		Topic:         record.Topic,
		Key:           record.Key,
		Payload:       record.Payload,
		Headers:       headersRaw,
		Status:        string(commonoutbox.StatusPending),
	})
	if err != nil {
		return commonoutbox.Record{}, fmt.Errorf("append outbox record: %w", mapAppendErr(err))
	}

	mapped, err := mapRecord(created)
	if err != nil {
		return commonoutbox.Record{}, fmt.Errorf("map appended outbox record: %w", err)
	}

	return mapped, nil
}

func (r *Repository) ClaimPending(ctx context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	claimedAt := time.Now().UTC()

	rows, err := r.queries.ClaimPendingOutboxRecords(ctx, sqlc.ClaimPendingOutboxRecordsParams{
		ClaimedAt:   sql.NullTime{Time: claimedAt, Valid: true},
		Before:      sql.NullTime{Time: params.Before.UTC(), Valid: true},
		StaleBefore: sql.NullTime{Time: claimedAt.Add(-claimLockTTL), Valid: true},
		LimitCount:  int32(params.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("claim pending outbox records: %w", err)
	}

	records := make([]commonoutbox.Record, 0, len(rows))
	for _, row := range rows {
		record, mapErr := mapRecord(row)
		if mapErr != nil {
			return nil, fmt.Errorf("map claimed outbox record %q: %w", row.ID.String(), mapErr)
		}

		records = append(records, record)
	}

	return records, nil
}

func (r *Repository) MarkPublished(ctx context.Context, params commonoutbox.MarkPublishedParams) error {
	if err := params.Validate(); err != nil {
		return err
	}

	affected, err := r.queries.MarkOutboxRecordPublished(ctx, sqlc.MarkOutboxRecordPublishedParams{
		ID:          params.ID,
		ClaimToken:  sql.NullTime{Time: params.ClaimToken.UTC(), Valid: true},
		PublishedAt: sql.NullTime{Time: params.PublishedAt.UTC(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("mark outbox record published: %w", err)
	}

	if affected == 0 {
		return commonoutbox.ErrPublishConflict
	}

	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, params commonoutbox.MarkFailedParams) error {
	if err := params.Validate(); err != nil {
		return err
	}

	affected, err := r.queries.MarkOutboxRecordFailed(ctx, sqlc.MarkOutboxRecordFailedParams{
		ID:            params.ID,
		ClaimToken:    sql.NullTime{Time: params.ClaimToken.UTC(), Valid: true},
		Attempt:       int32(params.Attempt),
		NextAttemptAt: sql.NullTime{Time: params.NextAttemptAt.UTC(), Valid: true},
		LastError:     params.LastError,
		UpdatedAt:     time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("mark outbox record failed: %w", err)
	}

	if affected == 0 {
		return commonoutbox.ErrPublishConflict
	}

	return nil
}

func mapRecord(record sqlc.OutboxRecord) (commonoutbox.Record, error) {
	headers := make(map[string]string)
	if len(record.Headers) > 0 {
		if err := json.Unmarshal(record.Headers, &headers); err != nil {
			return commonoutbox.Record{}, fmt.Errorf("unmarshal headers: %w", err)
		}
	}

	mapped := commonoutbox.Record{
		ID:            record.ID,
		EventID:       record.EventID,
		EventName:     record.EventName,
		AggregateType: record.AggregateType,
		AggregateID:   record.AggregateID,
		Topic:         record.Topic,
		Key:           record.Key,
		Payload:       record.Payload,
		Headers:       headers,
		Attempt:       int(record.Attempt),
		Status:        commonoutbox.Status(record.Status),
		LastError:     record.LastError,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}

	if record.NextAttemptAt.Valid {
		mapped.NextAttemptAt = record.NextAttemptAt.Time
	}

	if record.LockedAt.Valid {
		mapped.LockedAt = record.LockedAt.Time
	}

	if record.PublishedAt.Valid {
		mapped.PublishedAt = record.PublishedAt.Time
	}

	return mapped, nil
}

func mapAppendErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23505":
			return commonoutbox.ErrIdempotencyConflict
		}
	}

	return err
}

var _ commonoutbox.Repository = (*Repository)(nil)

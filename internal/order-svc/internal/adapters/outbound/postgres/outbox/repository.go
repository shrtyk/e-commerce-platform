package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/sqlc"
)

type querier interface {
	AppendOutboxRecord(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error)
	ClaimPendingOutboxRecords(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	ClaimStaleInProgressOutboxRecords(ctx context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	MarkOutboxRecordPublished(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error)
	MarkOutboxRecordRetryableFailure(ctx context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error)
	MarkOutboxRecordDead(ctx context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error)
}

type Repository struct {
	queries querier
}

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

	eventID, err := uuid.Parse(record.EventID)
	if err != nil {
		return commonoutbox.Record{}, fmt.Errorf("parse event id: %w", err)
	}

	created, err := r.queries.AppendOutboxRecord(ctx, sqlc.AppendOutboxRecordParams{
		EventID:       eventID,
		EventName:     record.EventName,
		AggregateType: record.AggregateType,
		AggregateID:   record.AggregateID,
		Topic:         record.Topic,
		Key:           record.Key,
		Payload:       record.Payload,
		Headers:       headersRaw,
		Status:        commonoutbox.StatusPending,
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
		ClaimedAt:  sql.NullTime{Time: claimedAt, Valid: true},
		LockedBy:   sql.NullString{String: params.LockedBy, Valid: true},
		Before:     params.Before.UTC(),
		LimitCount: int32(params.Limit),
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

func (r *Repository) ClaimStaleInProgress(ctx context.Context, params commonoutbox.ClaimStaleInProgressParams) ([]commonoutbox.Record, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	claimedAt := time.Now().UTC()

	rows, err := r.queries.ClaimStaleInProgressOutboxRecords(ctx, sqlc.ClaimStaleInProgressOutboxRecordsParams{
		ClaimedAt:   sql.NullTime{Time: claimedAt, Valid: true},
		LockedBy:    sql.NullString{String: params.LockedBy, Valid: true},
		StaleBefore: sql.NullTime{Time: params.StaleBefore.UTC(), Valid: true},
		LimitCount:  int32(params.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("claim stale outbox records: %w", err)
	}

	records := make([]commonoutbox.Record, 0, len(rows))
	for _, row := range rows {
		record, mapErr := mapRecord(row)
		if mapErr != nil {
			return nil, fmt.Errorf("map stale outbox record %q: %w", row.ID.String(), mapErr)
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
		LockedBy:    sql.NullString{String: params.LockedBy, Valid: true},
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

func (r *Repository) MarkRetryableFailure(ctx context.Context, params commonoutbox.MarkRetryableFailureParams) error {
	if err := params.Validate(); err != nil {
		return err
	}

	affected, err := r.queries.MarkOutboxRecordRetryableFailure(ctx, sqlc.MarkOutboxRecordRetryableFailureParams{
		ID:            params.ID,
		LockedBy:      sql.NullString{String: params.LockedBy, Valid: true},
		ClaimToken:    sql.NullTime{Time: params.ClaimToken.UTC(), Valid: true},
		Attempt:       int32(params.Attempt),
		NextAttemptAt: params.NextAttemptAt.UTC(),
		LastError:     sql.NullString{String: params.LastError, Valid: true},
		UpdatedAt:     time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("mark outbox record retryable failure: %w", err)
	}

	if affected == 0 {
		return commonoutbox.ErrPublishConflict
	}

	return nil
}

func (r *Repository) MarkDead(ctx context.Context, params commonoutbox.MarkDeadParams) error {
	if err := params.Validate(); err != nil {
		return err
	}

	affected, err := r.queries.MarkOutboxRecordDead(ctx, sqlc.MarkOutboxRecordDeadParams{
		ID:         params.ID,
		LockedBy:   sql.NullString{String: params.LockedBy, Valid: true},
		ClaimToken: sql.NullTime{Time: params.ClaimToken.UTC(), Valid: true},
		Attempt:    int32(params.Attempt),
		LastError:  sql.NullString{String: params.LastError, Valid: true},
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("mark outbox record dead: %w", err)
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
		EventID:       record.EventID.String(),
		EventName:     record.EventName,
		AggregateType: record.AggregateType,
		AggregateID:   record.AggregateID,
		Topic:         record.Topic,
		Key:           record.Key,
		Payload:       record.Payload,
		Headers:       headers,
		Attempt:       int(record.Attempt),
		MaxAttempts:   int(record.MaxAttempts),
		Status:        toOutboxStatus(record.Status),
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}

	mapped.NextAttemptAt = record.NextAttemptAt

	if record.LastError.Valid {
		mapped.LastError = record.LastError.String
	}

	if record.LockedAt.Valid {
		mapped.LockedAt = record.LockedAt.Time
	}

	if record.LockedBy.Valid {
		mapped.LockedBy = record.LockedBy.String
	}

	if record.PublishedAt.Valid {
		mapped.PublishedAt = record.PublishedAt.Time
	}

	return mapped, nil
}

func toOutboxStatus(value interface{}) commonoutbox.Status {
	switch typed := value.(type) {
	case string:
		return commonoutbox.Status(typed)
	case []byte:
		return commonoutbox.Status(string(typed))
	default:
		return commonoutbox.Status(fmt.Sprintf("%v", typed))
	}
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

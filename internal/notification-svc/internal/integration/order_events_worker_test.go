//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adapterkafka "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/inbound/kafka"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/events"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres/outbox"
	adapterpostgresrepos "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres/repos"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/provider"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/service/notification"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/testhelper"
)

const (
	integrationConsumerGroup = "notification-svc-order-events-v1"
	outboxTopic              = "notification.events"
)

func TestOrderEventsWorkerIntegration(t *testing.T) {
	t.Run("order confirmed creates sent notification and outbox records", func(t *testing.T) {
		stack := newCleanIntegrationStack(t)

		eventID := uuid.New()
		orderID := uuid.New()
		correlationID := "corr-confirmed"

		stack.consumer.SetMessages(buildOrderConfirmedMessage(eventID, orderID, "confirmed-user@example.com", correlationID))
		require.NoError(t, stack.worker.Tick(context.Background()))

		request := stack.queries.mustGetDeliveryRequestByEventID(t, eventID)
		require.Equal(t, domain.DeliveryStatusSent, request.Status)
		require.Equal(t, "order-confirmed", request.TemplateKey)

		attempts := stack.queries.mustListAttemptsByDeliveryRequestID(t, request.DeliveryRequestID)
		require.Len(t, attempts, 1)
		require.Equal(t, "stub-delivery", attempts[0].ProviderName)
		require.Empty(t, attempts[0].FailureCode)

		require.Equal(t, 1, stack.queries.mustCountConsumerIdempotencyByGroup(t, eventID, integrationConsumerGroup))
		require.Equal(t, 1, stack.queries.mustCountConsumerIdempotencyByGroup(t, eventID, integrationConsumerGroup+".delivery-results"))

		outboxRows := stack.queries.mustListOutboxByAggregateID(t, request.DeliveryRequestID)
		require.Len(t, outboxRows, 2)
		require.Equal(t, []string{"notification.delivery_requested", "notification.sent"}, outboxEventNames(outboxRows))
		for _, row := range outboxRows {
			require.Equal(t, outboxTopic, row.Topic)
			require.Equal(t, request.DeliveryRequestID.String(), string(row.Key))
			require.Equal(t, row.EventName, row.Headers[commonkafka.HeaderEventName])
			require.Equal(t, correlationID, row.Headers[commonkafka.HeaderCorrelationID])
		}
	})

	t.Run("order cancelled creates sent notification with cancelled body", func(t *testing.T) {
		stack := newCleanIntegrationStack(t)

		eventID := uuid.New()
		orderID := uuid.New()
		reason := "stock unavailable"

		stack.consumer.SetMessages(buildOrderCancelledMessage(eventID, orderID, "cancelled-user@example.com", "corr-cancelled", "stock_unavailable", reason))
		require.NoError(t, stack.worker.Tick(context.Background()))

		request := stack.queries.mustGetDeliveryRequestByEventID(t, eventID)
		require.Equal(t, "order-cancelled", request.TemplateKey)
		require.Equal(t, domain.DeliveryStatusSent, request.Status)

		sendInputs := stack.deliveryProvider.Inputs()
		require.Len(t, sendInputs, 1)
		require.Contains(t, strings.ToLower(sendInputs[0].Body), "cancelled")
		require.Contains(t, sendInputs[0].Body, reason)

		attempts := stack.queries.mustListAttemptsByDeliveryRequestID(t, request.DeliveryRequestID)
		require.Len(t, attempts, 1)

		outboxRows := stack.queries.mustListOutboxByAggregateID(t, request.DeliveryRequestID)
		require.Equal(t, []string{"notification.delivery_requested", "notification.sent"}, outboxEventNames(outboxRows))
	})

	t.Run("idempotent replay does not create duplicate work", func(t *testing.T) {
		stack := newCleanIntegrationStack(t)

		eventID := uuid.New()
		orderID := uuid.New()
		message := buildOrderConfirmedMessage(eventID, orderID, "replay-user@example.com", "corr-replay")

		stack.consumer.SetMessages(message)
		require.NoError(t, stack.worker.Tick(context.Background()))

		request := stack.queries.mustGetDeliveryRequestByEventID(t, eventID)

		stack.consumer.SetMessages(message)
		require.NoError(t, stack.worker.Tick(context.Background()))

		require.Equal(t, 1, stack.queries.mustCountDeliveryRequestsByEventID(t, eventID))
		require.Equal(t, 1, stack.queries.mustCountDeliveryAttemptsByRequestID(t, request.DeliveryRequestID))
		require.Equal(t, 2, stack.queries.mustCountOutboxByAggregateID(t, request.DeliveryRequestID))
		require.Equal(t, 1, stack.queries.mustCountConsumerIdempotencyByGroup(t, eventID, integrationConsumerGroup))
		require.Equal(t, 1, stack.queries.mustCountConsumerIdempotencyByGroup(t, eventID, integrationConsumerGroup+".delivery-results"))
	})

	t.Run("provider failure marks request failed and replay stays idempotent", func(t *testing.T) {
		stack := newCleanIntegrationStack(t)

		eventID := uuid.New()
		orderID := uuid.New()
		message := buildOrderConfirmedMessage(eventID, orderID, "failure-user@fail.test", "corr-failure")

		stack.consumer.SetMessages(message)
		require.NoError(t, stack.worker.Tick(context.Background()))

		request := stack.queries.mustGetDeliveryRequestByEventID(t, eventID)
		require.Equal(t, domain.DeliveryStatusFailed, request.Status)
		require.Equal(t, "recipient_suffix_fail", request.LastErrorCode)
		require.Contains(t, request.LastErrorMessage, "@fail.test")

		attempts := stack.queries.mustListAttemptsByDeliveryRequestID(t, request.DeliveryRequestID)
		require.Len(t, attempts, 1)
		require.Equal(t, "recipient_suffix_fail", attempts[0].FailureCode)

		outboxRows := stack.queries.mustListOutboxByAggregateID(t, request.DeliveryRequestID)
		require.Len(t, outboxRows, 2)
		require.Equal(t, []string{"notification.delivery_requested", "notification.failed"}, outboxEventNames(outboxRows))

		stack.consumer.SetMessages(message)
		require.NoError(t, stack.worker.Tick(context.Background()))

		require.Equal(t, 1, stack.queries.mustCountDeliveryRequestsByEventID(t, eventID))
		require.Equal(t, 1, stack.queries.mustCountDeliveryAttemptsByRequestID(t, request.DeliveryRequestID))
		require.Equal(t, 2, stack.queries.mustCountOutboxByAggregateID(t, request.DeliveryRequestID))
		require.Equal(t, 1, stack.queries.mustCountConsumerIdempotencyByGroup(t, eventID, integrationConsumerGroup))
		require.Equal(t, 1, stack.queries.mustCountConsumerIdempotencyByGroup(t, eventID, integrationConsumerGroup+".delivery-results"))
	})

	t.Run("retriable service error republishes to retry topic", func(t *testing.T) {
		stack := newCleanIntegrationStack(t)

		eventID := uuid.New()
		orderID := uuid.New()
		message := buildOrderConfirmedMessage(eventID, orderID, "retry-user@example.com", "corr-retry")
		message.Envelope.Key = []byte("order-key")
		message.Envelope.Payload = []byte("payload-body")

		stack.deliveryProvider.inner = failingDeliveryProvider{err: errors.New("provider unavailable")}
		stack.consumer.SetMessages(message)

		require.NoError(t, stack.worker.Tick(context.Background()))

		published := stack.publisher.Published()
		require.Len(t, published, 1)
		require.Equal(t, "order.events.retry", published[0].Topic)
		require.Equal(t, []byte("order-key"), published[0].Key)
		require.Equal(t, []byte("payload-body"), published[0].Payload)
		assertRetryHeadersPresent(t, published[0].Headers, retryHeaderPresenceExpectations{
			Attempt:       "1",
			MaxAttempts:   "3",
			OriginalTopic: "order.events",
			ErrorCode:     "NOTIFICATION_HANDLE_ORDER_EVENT_FAILED",
			ErrorMessage:  "send delivery: provider unavailable",
			ConsumerGroup: integrationConsumerGroup,
		})
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderRetryFirstFailedAt)
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderRetryLastFailedAt)
	})

	t.Run("invalid payload republishes to dlq topic", func(t *testing.T) {
		stack := newCleanIntegrationStack(t)

		message := buildOrderConfirmedMessage(uuid.New(), uuid.New(), "bad-user@example.com", "corr-dlq")
		message.Envelope.Key = []byte("order-key")
		message.Envelope.Payload = []byte("payload-body")
		message.Message = newOrderConfirmedProto("not-a-uuid", uuid.NewString(), "bad-user@example.com", "corr-dlq")

		stack.consumer.SetMessages(message)

		require.NoError(t, stack.worker.Tick(context.Background()))

		published := stack.publisher.Published()
		require.Len(t, published, 1)
		require.Equal(t, "order.events.dlq", published[0].Topic)
		require.Equal(t, []byte("order-key"), published[0].Key)
		require.Equal(t, []byte("payload-body"), published[0].Payload)
		assertRetryHeadersPresent(t, published[0].Headers, retryHeaderPresenceExpectations{
			Attempt:       "0",
			MaxAttempts:   "3",
			OriginalTopic: "order.events",
			ErrorCode:     "NOTIFICATION_INVALID_EVENT_PAYLOAD",
			ErrorMessage:  "parse order confirmed: parse event id: invalid UUID length: 10",
			ConsumerGroup: integrationConsumerGroup,
		})
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderRetryFirstFailedAt)
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderRetryLastFailedAt)
		require.Equal(t, commonkafka.DLQReasonNonRetryable, published[0].Headers[commonkafka.HeaderDLQReason])
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderDLQAt)
	})

	t.Run("unsupported order event message routes to dlq and keeps contract", func(t *testing.T) {
		stack := newCleanIntegrationStack(t)

		message := buildOrderConfirmedMessage(uuid.New(), uuid.New(), "unsupported-user@example.com", "corr-unsupported")
		message.Envelope.Key = []byte("order-key")
		message.Envelope.Payload = []byte("payload-body")
		message.Message = &commonv1.EventMetadata{}

		stack.consumer.SetMessages(message)

		require.NoError(t, stack.worker.Tick(context.Background()))

		published := stack.publisher.Published()
		require.Len(t, published, 1)
		require.Equal(t, "order.events.dlq", published[0].Topic)
		assertRetryHeadersPresent(t, published[0].Headers, retryHeaderPresenceExpectations{
			Attempt:       "0",
			MaxAttempts:   "3",
			OriginalTopic: "order.events",
			ErrorCode:     "NOTIFICATION_INVALID_EVENT_PAYLOAD",
			ErrorMessage:  "unsupported order event message: *commonv1.EventMetadata",
			ConsumerGroup: integrationConsumerGroup,
		})
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderRetryFirstFailedAt)
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderRetryLastFailedAt)
		require.Equal(t, commonkafka.DLQReasonNonRetryable, published[0].Headers[commonkafka.HeaderDLQReason])
		assertRFC3339TimestampHeader(t, published[0].Headers, commonkafka.HeaderDLQAt)
	})
}

type integrationStack struct {
	worker           *adapterkafka.OrderEventsWorker
	consumer         *queuedConsumer
	publisher        *capturingPublisher
	deliveryProvider *capturingDeliveryProvider
	queries          *dbAsserter
}

func newCleanIntegrationStack(t *testing.T) *integrationStack {
	t.Helper()

	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)

	consumer := &queuedConsumer{}
	publisher := &capturingPublisher{}
	deliveryProvider := &capturingDeliveryProvider{inner: provider.NewStubProvider()}

	deliveryRequestRepository := adapterpostgresrepos.NewDeliveryRequestRepository(harness.DB)
	deliveryAttemptRepository := adapterpostgresrepos.NewDeliveryAttemptRepository(harness.DB)
	consumerIdempotencyRepository := adapterpostgresrepos.NewConsumerIdempotencyRepository(harness.DB)
	outboxRepository := adapteroutbox.NewRepository(harness.DB)
	outboxPublisher := adapterevents.MustCreateOutboxEventPublisher(outboxRepository)

	txProvider := sqltx.NewProvider(harness.DB, func(tx *sql.Tx) notification.NotificationRepos {
		return notification.NotificationRepos{
			DeliveryRequests:      adapterpostgresrepos.NewDeliveryRequestRepositoryFromTx(tx),
			DeliveryAttempts:      adapterpostgresrepos.NewDeliveryAttemptRepositoryFromTx(tx),
			ConsumerIdempotencies: adapterpostgresrepos.NewConsumerIdempotencyRepositoryFromTx(tx),
			Publisher:             adapterevents.MustCreateOutboxEventPublisher(adapteroutbox.NewRepositoryFromTx(tx)),
		}
	})

	notificationService := notification.NewNotificationService(
		deliveryRequestRepository,
		deliveryAttemptRepository,
		consumerIdempotencyRepository,
	).WithEventPublisher(outboxPublisher, "notification-svc").
		WithTxProvider(txProvider).
		WithDeliveryProvider(deliveryProvider)

	worker, err := adapterkafka.NewOrderEventsWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		consumer,
		notificationService,
		publisher,
		adapterkafka.OrderEventsWorkerConfig{
			PollInterval:      time.Millisecond,
			ConsumerGroupName: integrationConsumerGroup,
			MaxRetryAttempts:  3,
		},
	)
	require.NoError(t, err)

	return &integrationStack{
		worker:           worker,
		consumer:         consumer,
		publisher:        publisher,
		deliveryProvider: deliveryProvider,
		queries:          &dbAsserter{db: harness.DB},
	}
}

type queuedConsumer struct {
	mu       sync.Mutex
	messages []commonkafka.ConsumedMessage
}

func (c *queuedConsumer) SetMessages(messages ...commonkafka.ConsumedMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.messages = append([]commonkafka.ConsumedMessage(nil), messages...)
}

func (c *queuedConsumer) Poll(context.Context) ([]commonkafka.ConsumedMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := append([]commonkafka.ConsumedMessage(nil), c.messages...)
	c.messages = nil

	return out, nil
}

func (c *queuedConsumer) CommitUncommittedOffsets(context.Context) error {
	return nil
}

type capturingPublisher struct {
	mu        sync.Mutex
	envelopes []commonkafka.EventEnvelope
}

func (p *capturingPublisher) Publish(_ context.Context, envelope commonkafka.EventEnvelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.envelopes = append(p.envelopes, envelope)

	return nil
}

func (p *capturingPublisher) Published() []commonkafka.EventEnvelope {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]commonkafka.EventEnvelope(nil), p.envelopes...)
}

type capturingDeliveryProvider struct {
	mu     sync.Mutex
	inner  outbound.DeliveryProvider
	inputs []outbound.SendDeliveryInput
}

type failingDeliveryProvider struct {
	err error
}

func (p failingDeliveryProvider) Send(context.Context, outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
	return outbound.SendDeliveryResult{}, p.err
}

func (p *capturingDeliveryProvider) Send(ctx context.Context, input outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
	p.mu.Lock()
	p.inputs = append(p.inputs, input)
	p.mu.Unlock()

	return p.inner.Send(ctx, input)
}

func (p *capturingDeliveryProvider) Inputs() []outbound.SendDeliveryInput {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]outbound.SendDeliveryInput(nil), p.inputs...)
}

type outboxRow struct {
	EventName string
	Topic     string
	Key       []byte
	Headers   map[string]string
}

type dbAsserter struct {
	db *sql.DB
}

func (q *dbAsserter) mustGetDeliveryRequestByEventID(t *testing.T, eventID uuid.UUID) domain.DeliveryRequest {
	t.Helper()

	const query = `
		SELECT
			dr.delivery_request_id,
			dr.source_event_id,
			dr.correlation_id,
			dr.source_event_name,
			dr.channel,
			dr.recipient,
			dr.template_key,
			dr.status::text,
			dr.idempotency_key,
			dr.last_error_code,
			dr.last_error_message,
			dr.created_at,
			dr.updated_at
		FROM delivery_requests dr
		INNER JOIN consumer_idempotency ci ON ci.delivery_request_id = dr.delivery_request_id
		WHERE ci.event_id = $1
		ORDER BY ci.created_at ASC, ci.consumer_group_name ASC
		LIMIT 1
	`

	var (
		request          domain.DeliveryRequest
		status           string
		lastErrorCode    sql.NullString
		lastErrorMessage sql.NullString
	)

	err := q.db.QueryRowContext(context.Background(), query, eventID).Scan(
		&request.DeliveryRequestID,
		&request.SourceEventID,
		&request.CorrelationID,
		&request.SourceEventName,
		&request.Channel,
		&request.Recipient,
		&request.TemplateKey,
		&status,
		&request.IdempotencyKey,
		&lastErrorCode,
		&lastErrorMessage,
		&request.CreatedAt,
		&request.UpdatedAt,
	)
	require.NoError(t, err)

	request.Status = domain.DeliveryStatus(status)
	request.LastErrorCode = lastErrorCode.String
	request.LastErrorMessage = lastErrorMessage.String

	return request
}

func (q *dbAsserter) mustListAttemptsByDeliveryRequestID(t *testing.T, deliveryRequestID uuid.UUID) []domain.DeliveryAttempt {
	t.Helper()

	const query = `
		SELECT
			delivery_attempt_id,
			delivery_request_id,
			attempt_number,
			provider_name,
			provider_message_id,
			failure_code,
			failure_message,
			attempted_at
		FROM delivery_attempts
		WHERE delivery_request_id = $1
		ORDER BY attempt_number ASC
	`

	rows, err := q.db.QueryContext(context.Background(), query, deliveryRequestID)
	require.NoError(t, err)
	defer rows.Close()

	attempts := make([]domain.DeliveryAttempt, 0)
	for rows.Next() {
		var (
			attempt        domain.DeliveryAttempt
			failureCode    sql.NullString
			failureMessage sql.NullString
		)

		err := rows.Scan(
			&attempt.DeliveryAttemptID,
			&attempt.DeliveryRequestID,
			&attempt.AttemptNumber,
			&attempt.ProviderName,
			&attempt.ProviderMessageID,
			&failureCode,
			&failureMessage,
			&attempt.AttemptedAt,
		)
		require.NoError(t, err)

		attempt.FailureCode = failureCode.String
		attempt.FailureMessage = failureMessage.String

		attempts = append(attempts, attempt)
	}
	require.NoError(t, rows.Err())

	return attempts
}

func (q *dbAsserter) mustCountDeliveryRequestsByEventID(t *testing.T, eventID uuid.UUID) int {
	t.Helper()

	const query = `
		SELECT COUNT(DISTINCT delivery_request_id)
		FROM consumer_idempotency
		WHERE event_id = $1
	`

	var count int
	err := q.db.QueryRowContext(context.Background(), query, eventID).Scan(&count)
	require.NoError(t, err)

	return count
}

func (q *dbAsserter) mustCountDeliveryAttemptsByRequestID(t *testing.T, deliveryRequestID uuid.UUID) int {
	t.Helper()

	const query = `SELECT COUNT(*) FROM delivery_attempts WHERE delivery_request_id = $1`

	var count int
	err := q.db.QueryRowContext(context.Background(), query, deliveryRequestID).Scan(&count)
	require.NoError(t, err)

	return count
}

func (q *dbAsserter) mustCountConsumerIdempotencyByGroup(t *testing.T, eventID uuid.UUID, groupName string) int {
	t.Helper()

	const query = `SELECT COUNT(*) FROM consumer_idempotency WHERE event_id = $1 AND consumer_group_name = $2`

	var count int
	err := q.db.QueryRowContext(context.Background(), query, eventID, groupName).Scan(&count)
	require.NoError(t, err)

	return count
}

func (q *dbAsserter) mustListOutboxByAggregateID(t *testing.T, aggregateID uuid.UUID) []outboxRow {
	t.Helper()

	const query = `
		SELECT event_name, topic, key, headers
		FROM outbox_records
		WHERE aggregate_id = $1
		ORDER BY created_at ASC, id ASC
	`

	rows, err := q.db.QueryContext(context.Background(), query, aggregateID.String())
	require.NoError(t, err)
	defer rows.Close()

	out := make([]outboxRow, 0)
	for rows.Next() {
		var (
			row        outboxRow
			headersRaw []byte
		)

		err := rows.Scan(&row.EventName, &row.Topic, &row.Key, &headersRaw)
		require.NoError(t, err)

		err = json.Unmarshal(headersRaw, &row.Headers)
		require.NoError(t, err)

		out = append(out, row)
	}
	require.NoError(t, rows.Err())

	return out
}

func (q *dbAsserter) mustCountOutboxByAggregateID(t *testing.T, aggregateID uuid.UUID) int {
	t.Helper()

	const query = `SELECT COUNT(*) FROM outbox_records WHERE aggregate_id = $1`

	var count int
	err := q.db.QueryRowContext(context.Background(), query, aggregateID.String()).Scan(&count)
	require.NoError(t, err)

	return count
}

func buildOrderConfirmedMessage(eventID uuid.UUID, orderID uuid.UUID, userID string, correlationID string) commonkafka.ConsumedMessage {
	return commonkafka.ConsumedMessage{
		Envelope: commonkafka.EventEnvelope{
			Topic: "order.events",
			Metadata: commonkafka.EventMetadata{
				EventID:       eventID.String(),
				EventName:     "order.confirmed",
				Producer:      "order-svc",
				OccurredAt:    time.Now().UTC(),
				CorrelationID: correlationID,
				SchemaVersion: "1",
			},
		},
		Message: &orderv1.OrderConfirmed{
			Metadata: &commonv1.EventMetadata{
				EventId:       eventID.String(),
				EventName:     "order.confirmed",
				Producer:      "order-svc",
				OccurredAt:    timestamppb.New(time.Now().UTC()),
				CorrelationId: correlationID,
				SchemaVersion: "1",
			},
			OrderId: orderID.String(),
			UserId:  userID,
		},
	}
}

func newOrderConfirmedProto(eventID string, orderID string, userID string, correlationID string) *orderv1.OrderConfirmed {
	return &orderv1.OrderConfirmed{
		Metadata: &commonv1.EventMetadata{
			EventId:       eventID,
			EventName:     "order.confirmed",
			Producer:      "order-svc",
			OccurredAt:    timestamppb.New(time.Now().UTC()),
			CorrelationId: correlationID,
			SchemaVersion: "1",
		},
		OrderId: orderID,
		UserId:  userID,
	}
}

func buildOrderCancelledMessage(eventID uuid.UUID, orderID uuid.UUID, userID string, correlationID string, reasonCode string, reasonMessage string) commonkafka.ConsumedMessage {
	return commonkafka.ConsumedMessage{
		Envelope: commonkafka.EventEnvelope{
			Topic: "order.events",
			Metadata: commonkafka.EventMetadata{
				EventID:       eventID.String(),
				EventName:     "order.cancelled",
				Producer:      "order-svc",
				OccurredAt:    time.Now().UTC(),
				CorrelationID: correlationID,
				SchemaVersion: "1",
			},
		},
		Message: &orderv1.OrderCancelled{
			Metadata: &commonv1.EventMetadata{
				EventId:       eventID.String(),
				EventName:     "order.cancelled",
				Producer:      "order-svc",
				OccurredAt:    timestamppb.New(time.Now().UTC()),
				CorrelationId: correlationID,
				SchemaVersion: "1",
			},
			OrderId:             orderID.String(),
			UserId:              userID,
			CancelReasonCode:    reasonCode,
			CancelReasonMessage: reasonMessage,
		},
	}
}

func outboxEventNames(rows []outboxRow) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.EventName)
	}

	return names
}

type retryHeaderPresenceExpectations struct {
	Attempt       string
	MaxAttempts   string
	OriginalTopic string
	ErrorCode     string
	ErrorMessage  string
	ConsumerGroup string
}

func assertRetryHeadersPresent(t *testing.T, headers map[string]string, expected retryHeaderPresenceExpectations) {
	t.Helper()

	require.Equal(t, expected.Attempt, headers[commonkafka.HeaderRetryAttempt])
	require.Equal(t, expected.MaxAttempts, headers[commonkafka.HeaderRetryMaxAttempts])
	require.Equal(t, expected.OriginalTopic, headers[commonkafka.HeaderRetryOriginalTopic])
	require.Equal(t, expected.ErrorCode, headers[commonkafka.HeaderRetryErrorCode])
	require.Equal(t, expected.ErrorMessage, headers[commonkafka.HeaderRetryErrorMessage])
	require.Equal(t, expected.ConsumerGroup, headers[commonkafka.HeaderRetryConsumerGroup])
}

func assertRFC3339TimestampHeader(t *testing.T, headers map[string]string, key string) {
	t.Helper()

	raw := headers[key]
	require.NotEmpty(t, raw)

	_, err := time.Parse(time.RFC3339, raw)
	require.NoError(t, err)
}

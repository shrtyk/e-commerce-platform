package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/service/payment"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPaymentServerInitiatePayment(t *testing.T) {
	orderID := uuid.New()
	attemptID := uuid.New()

	t.Run("returns invalid argument for malformed request", func(t *testing.T) {
		server := NewPaymentServer(stubPaymentService{}, testLogger())

		_, err := server.InitiatePayment(context.Background(), &paymentv1.InitiatePaymentRequest{})

		require.Error(t, err)
		require.Equal(t, grpccodes.InvalidArgument, status.Code(err))
	})

	t.Run("maps success response", func(t *testing.T) {
		processedAt := time.Now().UTC()

		server := NewPaymentServer(stubPaymentService{
			initiatePaymentFunc: func(_ context.Context, input payment.InitiatePaymentInput) (payment.InitiatePaymentResult, error) {
				require.Equal(t, orderID, input.OrderID)
				require.Equal(t, int64(2500), input.Amount)
				require.Equal(t, "USD", input.Currency)

				return payment.InitiatePaymentResult{PaymentAttempt: domain.PaymentAttempt{
					PaymentAttemptID: attemptID,
					OrderID:          input.OrderID,
					Status:           domain.PaymentStatusInitiated,
					Amount:           input.Amount,
					Currency:         input.Currency,
					ProviderName:     input.ProviderName,
					IdempotencyKey:   input.IdempotencyKey,
					FailureCode:      "declined",
					FailureMessage:   "insufficient funds",
					ProcessedAt:      &processedAt,
				}}, nil
			},
		}, testLogger())

		resp, err := server.InitiatePayment(context.Background(), &paymentv1.InitiatePaymentRequest{
			OrderId: orderID.String(),
			Amount: &commonv1.Money{
				Amount:   2500,
				Currency: "USD",
			},
			ProviderName:   "stub",
			IdempotencyKey: "idem-1",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, attemptID.String(), resp.GetPaymentAttempt().GetPaymentAttemptId())
		require.Equal(t, paymentv1.PaymentStatus_PAYMENT_STATUS_INITIATED, resp.GetPaymentAttempt().GetStatus())

		message := resp.GetPaymentAttempt().ProtoReflect()

		failureCodeField := message.Descriptor().Fields().ByName("failure_code")
		if failureCodeField != nil {
			require.Equal(t, "declined", message.Get(failureCodeField).String())
		}

		failureMessageField := message.Descriptor().Fields().ByName("failure_message")
		if failureMessageField != nil {
			require.Equal(t, "insufficient funds", message.Get(failureMessageField).String())
		}

		processedAtField := message.Descriptor().Fields().ByName("processed_at")
		if processedAtField != nil {
			require.True(t, message.Has(processedAtField))

			gotProcessedAt := message.Get(processedAtField).Message().Interface().(*timestamppb.Timestamp)
			require.Equal(t, processedAt.Unix(), gotProcessedAt.AsTime().Unix())
		}
	})

	t.Run("maps duplicate to already exists", func(t *testing.T) {
		server := NewPaymentServer(stubPaymentService{
			initiatePaymentFunc: func(context.Context, payment.InitiatePaymentInput) (payment.InitiatePaymentResult, error) {
				return payment.InitiatePaymentResult{}, payment.ErrPaymentAttemptDuplicate
			},
		}, testLogger())

		_, err := server.InitiatePayment(context.Background(), &paymentv1.InitiatePaymentRequest{
			OrderId: orderID.String(),
			Amount: &commonv1.Money{
				Amount:   2500,
				Currency: "USD",
			},
			ProviderName:   "stub",
			IdempotencyKey: "idem-1",
		})

		require.Error(t, err)
		require.Equal(t, grpccodes.AlreadyExists, status.Code(err))
	})
}

func TestPaymentServerInitiatePaymentInternalErrorLogsEnrichedFields(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	requestID := "req-payment-1"
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
		SpanID:     trace.SpanID{6, 6, 6, 6, 6, 6, 6, 6},
		TraceFlags: trace.FlagsSampled,
	})

	ctx := transport.WithRequestID(context.Background(), requestID)
	ctx = trace.ContextWithSpanContext(ctx, spanContext)

	orderID := uuid.New()
	server := NewPaymentServer(stubPaymentService{
		initiatePaymentFunc: func(context.Context, payment.InitiatePaymentInput) (payment.InitiatePaymentResult, error) {
			return payment.InitiatePaymentResult{}, errors.New("provider timeout")
		},
	}, logger)

	resp, err := server.InitiatePayment(ctx, &paymentv1.InitiatePaymentRequest{
		OrderId: orderID.String(),
		Amount: &commonv1.Money{
			Amount:   1000,
			Currency: "USD",
		},
		ProviderName:   "stub",
		IdempotencyKey: "idem-1",
	})

	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, grpccodes.Internal, status.Code(err))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))

	require.Equal(t, "grpc internal error", entry["msg"])
	require.Equal(t, "payment-svc", entry["service"])
	require.Equal(t, requestID, entry["request_id"])
	require.Equal(t, spanContext.TraceID().String(), entry["trace_id"])
	require.Equal(t, "InitiatePayment", entry["method"])
	require.Equal(t, paymentv1.PaymentService_InitiatePayment_FullMethodName, entry["path"])
	require.Equal(t, float64(grpccodes.Internal), entry["status"])
	require.Equal(t, grpccodes.Internal.String(), entry["grpc_status"])
	require.Equal(t, "provider timeout", entry["error"])
	require.Equal(t, orderID.String(), entry["order_id"])
}

func TestPaymentServerInitiatePaymentInternalErrorLogsFallbackMetadata(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	orderID := uuid.New()
	server := NewPaymentServer(stubPaymentService{
		initiatePaymentFunc: func(context.Context, payment.InitiatePaymentInput) (payment.InitiatePaymentResult, error) {
			return payment.InitiatePaymentResult{}, errors.New("provider timeout")
		},
	}, logger)

	require.NotPanics(t, func() {
		resp, err := server.InitiatePayment(context.Background(), &paymentv1.InitiatePaymentRequest{
			OrderId: orderID.String(),
			Amount: &commonv1.Money{
				Amount:   1000,
				Currency: "USD",
			},
			ProviderName:   "stub",
			IdempotencyKey: "idem-1",
		})

		require.Nil(t, resp)
		require.Error(t, err)
		require.Equal(t, grpccodes.Internal, status.Code(err))
	})

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))

	require.Equal(t, "", entry["request_id"])
	require.Equal(t, "", entry["trace_id"])
	require.Equal(t, grpccodes.Internal.String(), entry["grpc_status"])
}

type stubPaymentService struct {
	initiatePaymentFunc func(context.Context, payment.InitiatePaymentInput) (payment.InitiatePaymentResult, error)
}

func (s stubPaymentService) InitiatePayment(ctx context.Context, input payment.InitiatePaymentInput) (payment.InitiatePaymentResult, error) {
	if s.initiatePaymentFunc == nil {
		return payment.InitiatePaymentResult{}, errors.New("initiate payment is not configured")
	}

	return s.initiatePaymentFunc(ctx, input)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

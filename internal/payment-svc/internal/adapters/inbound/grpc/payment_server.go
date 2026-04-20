package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/service/payment"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type paymentService interface {
	InitiatePayment(ctx context.Context, input payment.InitiatePaymentInput) (payment.InitiatePaymentResult, error)
}

type PaymentServer struct {
	paymentv1.UnimplementedPaymentServiceServer

	service paymentService
	logger  *slog.Logger
}

func NewPaymentServer(service paymentService, logger *slog.Logger) *PaymentServer {
	if logger == nil {
		logger = slog.Default()
	}

	return &PaymentServer{service: service, logger: logger}
}

func (s *PaymentServer) InitiatePayment(
	ctx context.Context,
	req *paymentv1.InitiatePaymentRequest,
) (*paymentv1.InitiatePaymentResponse, error) {
	if s.service == nil {
		return nil, status.Error(grpccodes.Internal, "payment service is not configured")
	}

	input, err := toCreatePaymentAttemptInput(req)
	if err != nil {
		return nil, status.Error(grpccodes.InvalidArgument, err.Error())
	}

	result, err := s.service.InitiatePayment(ctx, input)
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return &paymentv1.InitiatePaymentResponse{PaymentAttempt: toProtoPaymentAttempt(result.PaymentAttempt)}, nil
}

func toCreatePaymentAttemptInput(req *paymentv1.InitiatePaymentRequest) (payment.InitiatePaymentInput, error) {
	if req == nil {
		return payment.InitiatePaymentInput{}, fmt.Errorf("request is required")
	}

	orderID, err := uuid.Parse(req.GetOrderId())
	if err != nil || orderID == uuid.Nil {
		return payment.InitiatePaymentInput{}, fmt.Errorf("invalid order id")
	}

	amount := req.GetAmount()
	if amount == nil || amount.GetAmount() <= 0 || amount.GetCurrency() == "" {
		return payment.InitiatePaymentInput{}, fmt.Errorf("invalid amount")
	}

	if req.GetProviderName() == "" {
		return payment.InitiatePaymentInput{}, fmt.Errorf("provider name is required")
	}

	if req.GetIdempotencyKey() == "" {
		return payment.InitiatePaymentInput{}, fmt.Errorf("idempotency key is required")
	}

	return payment.InitiatePaymentInput{
		OrderID:        orderID,
		Amount:         amount.GetAmount(),
		Currency:       amount.GetCurrency(),
		ProviderName:   req.GetProviderName(),
		IdempotencyKey: req.GetIdempotencyKey(),
	}, nil
}

func toProtoPaymentAttempt(attempt domain.PaymentAttempt) *paymentv1.PaymentAttempt {
	protoAttempt := &paymentv1.PaymentAttempt{
		PaymentAttemptId:  attempt.PaymentAttemptID.String(),
		OrderId:           attempt.OrderID.String(),
		Status:            toProtoPaymentStatus(attempt.Status),
		Amount:            &commonv1.Money{Amount: attempt.Amount, Currency: attempt.Currency},
		ProviderName:      attempt.ProviderName,
		ProviderReference: attempt.ProviderReference,
		IdempotencyKey:    attempt.IdempotencyKey,
	}

	setOptionalFailureFields(protoAttempt, attempt)

	return protoAttempt
}

func setOptionalFailureFields(protoAttempt *paymentv1.PaymentAttempt, attempt domain.PaymentAttempt) {
	message := protoAttempt.ProtoReflect()

	if field := message.Descriptor().Fields().ByName("failure_code"); field != nil {
		if field.Kind() == protoreflect.StringKind {
			message.Set(field, protoreflect.ValueOfString(attempt.FailureCode))
		}
	}

	if field := message.Descriptor().Fields().ByName("failure_message"); field != nil {
		if field.Kind() == protoreflect.StringKind {
			message.Set(field, protoreflect.ValueOfString(attempt.FailureMessage))
		}
	}

	if attempt.ProcessedAt == nil {
		return
	}

	if field := message.Descriptor().Fields().ByName("processed_at"); field != nil {
		if field.Kind() == protoreflect.MessageKind &&
			field.Message() != nil &&
			field.Message().FullName() == "google.protobuf.Timestamp" {
			processedAt := timestamppb.New(attempt.ProcessedAt.UTC())
			message.Set(field, protoreflect.ValueOfMessage(processedAt.ProtoReflect()))
		}
	}
}

func toProtoPaymentStatus(status domain.PaymentStatus) paymentv1.PaymentStatus {
	switch status {
	case domain.PaymentStatusInitiated:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_INITIATED
	case domain.PaymentStatusProcessing:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_PROCESSING
	case domain.PaymentStatusSucceeded:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_SUCCEEDED
	case domain.PaymentStatusFailed:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_FAILED
	default:
		return paymentv1.PaymentStatus_PAYMENT_STATUS_UNSPECIFIED
	}
}

func (s *PaymentServer) mapServiceError(err error) error {
	switch {
	case errors.Is(err, payment.ErrInvalidPaymentInput):
		return status.Error(grpccodes.InvalidArgument, "invalid payment input")
	case errors.Is(err, payment.ErrPaymentAttemptDuplicate):
		return status.Error(grpccodes.AlreadyExists, "payment attempt already exists")
	default:
		s.logger.Error("grpc internal error", "error", err.Error())
		return status.Error(grpccodes.Internal, "internal error")
	}
}

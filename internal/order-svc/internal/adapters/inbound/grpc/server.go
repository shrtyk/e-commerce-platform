package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonauth "github.com/shrtyk/e-commerce-platform/internal/common/auth"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

var publicMethods []string

func NewServer(
	logger *slog.Logger,
	serviceName string,
	checkoutService checkoutService,
	tokenVerifier httpcommon.TokenVerifier,
	tracer trace.Tracer,
) *grpcpkg.Server {
	if tokenVerifier == nil {
		panic("grpc token verifier is required")
	}

	interceptorsProvider := grpccommon.NewInterceptorsProviderWithTracer(serviceName, logger, tracer)

	server := grpcpkg.NewServer(
		grpcpkg.ChainUnaryInterceptor(
			interceptorsProvider.UnaryTracing(),
			interceptorsProvider.UnaryLogging(),
			interceptorsProvider.UnaryRecovery(),
			commonauth.UnaryAuthInterceptor(
				newGRPCTokenVerifier(tokenVerifier),
				func(ctx context.Context, claims commonauth.Claims) context.Context {
					return transport.WithClaims(ctx, transport.Claims{
						UserID: claims.UserID,
						Role:   string(claims.Role),
						Status: string(claims.Status),
					})
				},
				publicMethods,
			),
		),
	)

	orderv1.RegisterOrderServiceServer(server, NewOrderServer(checkoutService, logger))

	return server
}

type OrderServer struct {
	orderv1.UnimplementedOrderServiceServer

	checkoutService checkoutService
	logger          *slog.Logger
}

type checkoutService interface {
	Checkout(ctx context.Context, input checkout.CheckoutInput) (outbound.Order, error)
}

func NewOrderServer(checkoutService checkoutService, logger *slog.Logger) *OrderServer {
	if logger == nil {
		logger = slog.Default()
	}

	return &OrderServer{checkoutService: checkoutService, logger: logger}
}

func (s *OrderServer) CreateOrder(ctx context.Context, req *orderv1.CreateOrderRequest) (*orderv1.CreateOrderResponse, error) {
	claims, ok := transport.ClaimsFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing auth claims")
	}

	if s.checkoutService == nil {
		return nil, status.Error(codes.Internal, "checkout service is not configured")
	}

	requestedUserID, err := toUserID(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user id")
	}

	if requestedUserID != claims.UserID {
		return nil, status.Error(codes.PermissionDenied, "request user id mismatch")
	}

	input := checkout.CheckoutInput{
		UserID:         requestedUserID,
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
	}
	if paymentMethod := strings.TrimSpace(req.GetPaymentMethod()); paymentMethod != "" {
		input.PaymentMethod = &paymentMethod
	}

	order, err := s.checkoutService.Checkout(ctx, input)
	if err != nil {
		return nil, mapCheckoutGRPCError(err)
	}

	return &orderv1.CreateOrderResponse{Order: mapProtoOrder(order)}, nil
}

func (s *OrderServer) GetOrder(context.Context, *orderv1.GetOrderRequest) (*orderv1.GetOrderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method GetOrder not implemented")
}

func mapCheckoutGRPCError(err error) error {
	code := checkout.CodeOf(err)
	switch code {
	case checkout.CheckoutErrorCodeInvalidArgument:
		return status.Error(codes.InvalidArgument, string(code))
	case checkout.CheckoutErrorCodeCartNotFound, checkout.CheckoutErrorCodeSKUNotFound:
		return status.Error(codes.NotFound, string(code))
	case checkout.CheckoutErrorCodeCartEmpty:
		return status.Error(codes.FailedPrecondition, string(code))
	case checkout.CheckoutErrorCodeStockUnavailable,
		checkout.CheckoutErrorCodeWrongIdempotencyKeyPayload,
		checkout.CheckoutErrorCodeConflict:
		return status.Error(codes.Aborted, string(code))
	case checkout.CheckoutErrorCodePaymentDeclined:
		return status.Error(codes.PermissionDenied, string(code))
	default:
		return status.Error(codes.Internal, string(checkout.CheckoutErrorCodeInternal))
	}
}

func mapProtoOrder(order outbound.Order) *orderv1.Order {
	items := make([]*orderv1.OrderItem, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, &orderv1.OrderItem{
			OrderItemId: item.OrderItemID.String(),
			ProductId:   item.ProductID.String(),
			Sku:         item.SKU,
			Name:        item.Name,
			Quantity:    int64(item.Quantity),
			UnitPrice:   &commonv1.Money{Amount: item.UnitPrice, Currency: item.Currency},
			LineTotal:   &commonv1.Money{Amount: item.LineTotal, Currency: item.Currency},
		})
	}

	return &orderv1.Order{
		OrderId:     order.OrderID.String(),
		UserId:      order.UserID.String(),
		Status:      mapProtoOrderStatus(order.Status),
		Currency:    order.Currency,
		TotalAmount: &commonv1.Money{Amount: order.TotalAmount, Currency: order.Currency},
		Items:       items,
	}
}

func mapProtoOrderStatus(status outbound.OrderStatus) orderv1.OrderStatus {
	switch status {
	case outbound.OrderStatusPending:
		return orderv1.OrderStatus_ORDER_STATUS_PENDING
	case outbound.OrderStatusAwaitingStock:
		return orderv1.OrderStatus_ORDER_STATUS_AWAITING_STOCK
	case outbound.OrderStatusAwaitingPayment:
		return orderv1.OrderStatus_ORDER_STATUS_AWAITING_PAYMENT
	case outbound.OrderStatusConfirmed:
		return orderv1.OrderStatus_ORDER_STATUS_CONFIRMED
	case outbound.OrderStatusCancelled:
		return orderv1.OrderStatus_ORDER_STATUS_CANCELLED
	default:
		return orderv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func toUserID(raw string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return uuid.Nil, errors.New("missing user id")
	}

	parsed, err := uuid.Parse(trimmed)
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, errors.New("invalid user id")
	}

	return parsed, nil
}

type grpcTokenVerifier struct {
	tokenVerifier httpcommon.TokenVerifier
}

func newGRPCTokenVerifier(tokenVerifier httpcommon.TokenVerifier) commonauth.TokenVerifier {
	return grpcTokenVerifier{tokenVerifier: tokenVerifier}
}

func (v grpcTokenVerifier) Verify(token string) (commonauth.Claims, error) {
	claims, err := v.tokenVerifier.Verify(token)
	if err != nil {
		return commonauth.Claims{}, err
	}

	return transport.ToAuthClaims(claims)
}

package http

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/render"
	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

const defaultReadyzTimeout = time.Second
const maxIdempotencyKeyLength = 255

type readinessChecker interface {
	PingContext(context.Context) error
}

type checkoutService interface {
	Checkout(ctx context.Context, input checkout.CheckoutInput) (outbound.Order, error)
}

type OrderHandler struct {
	readinessChecker readinessChecker
	readinessTimeout time.Duration
	checkoutService  checkoutService
}

func NewOrderHandler(readinessChecker readinessChecker, readinessTimeout time.Duration, checkoutService checkoutService) *OrderHandler {
	if readinessTimeout <= 0 {
		readinessTimeout = defaultReadyzTimeout
	}

	return &OrderHandler{
		readinessChecker: readinessChecker,
		readinessTimeout: readinessTimeout,
		checkoutService:  checkoutService,
	}
}

func (h *OrderHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *OrderHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	if h.readinessChecker == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.readinessTimeout)
	defer cancel()

	if err := h.readinessChecker.PingContext(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	claims, ok := transport.ClaimsFromContext(r.Context())
	if !ok {
		h.writeError(w, r, commonerrors.Unauthorized("unauthorized", "unauthorized"))
		return
	}

	if h.checkoutService == nil {
		h.writeError(w, r, commonerrors.InternalError("INTERNAL"))
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		h.writeError(w, r, commonerrors.BadRequest(string(checkout.CheckoutErrorCodeInvalidArgument), "idempotency key is required"))
		return
	}

	if len(idempotencyKey) > maxIdempotencyKeyLength {
		h.writeError(w, r, commonerrors.BadRequest(string(checkout.CheckoutErrorCodeInvalidArgument), "idempotency key is too long"))
		return
	}

	var request dto.CreateOrderRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest(string(checkout.CheckoutErrorCodeInvalidArgument), "invalid request body"))
		return
	}

	result, err := h.checkoutService.Checkout(r.Context(), checkout.CheckoutInput{
		UserID:         claims.UserID,
		IdempotencyKey: idempotencyKey,
		PaymentMethod:  request.PaymentMethod,
	})
	if err != nil {
		h.writeError(w, r, mapCheckoutHTTPError(err))
		return
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, mapOrderDTO(result))
}

func (h *OrderHandler) GetOrderById(w http.ResponseWriter, _ *http.Request, _ dto.OrderId) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (h *OrderHandler) HandleOpenAPIError(w http.ResponseWriter, r *http.Request, _ error) {
	h.writeError(w, r, commonerrors.BadRequest(string(checkout.CheckoutErrorCodeInvalidArgument), "invalid request parameters"))
}

func (h *OrderHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httpErr := commonerrors.FromError(err)
	commonerrors.WriteJSON(w, httpErr, transport.RequestIDFromContext(r.Context()))
}

func mapCheckoutHTTPError(err error) error {
	code := checkout.CodeOf(err)
	if code == "" {
		return commonerrors.InternalError(string(checkout.CheckoutErrorCodeInternal))
	}

	switch code {
	case checkout.CheckoutErrorCodeInvalidArgument:
		return commonerrors.BadRequest(string(code), string(code))
	case checkout.CheckoutErrorCodeCartNotFound, checkout.CheckoutErrorCodeSKUNotFound:
		return commonerrors.NotFound(string(code), string(code))
	case checkout.CheckoutErrorCodeCartEmpty,
		checkout.CheckoutErrorCodeStockUnavailable,
		checkout.CheckoutErrorCodePaymentDeclined,
		checkout.CheckoutErrorCodeWrongIdempotencyKeyPayload,
		checkout.CheckoutErrorCodeConflict:
		return commonerrors.Conflict(string(code), string(code))
	default:
		return commonerrors.InternalError(string(checkout.CheckoutErrorCodeInternal))
	}
}

func mapOrderDTO(order outbound.Order) dto.Order {
	items := make([]dto.OrderItem, 0, len(order.Items))
	for _, item := range order.Items {
		sku := item.SKU
		name := item.Name
		currency := item.Currency

		items = append(items, dto.OrderItem{
			ProductId: item.ProductID.String(),
			Sku:       &sku,
			Name:      &name,
			Quantity:  toInt(item.Quantity),
			UnitPrice: toInt(item.UnitPrice),
			LineTotal: toInt(item.LineTotal),
			Currency:  &currency,
		})
	}

	return dto.Order{
		OrderId:     order.OrderID.String(),
		UserId:      order.UserID.String(),
		Status:      dto.OrderStatus(order.Status),
		Currency:    order.Currency,
		TotalAmount: toInt(order.TotalAmount),
		Items:       items,
	}
}

func toInt[T ~int64 | ~int32](value T) int {
	if int64(value) > math.MaxInt {
		return math.MaxInt
	}

	if int64(value) < math.MinInt {
		return math.MinInt
	}

	return int(value)
}

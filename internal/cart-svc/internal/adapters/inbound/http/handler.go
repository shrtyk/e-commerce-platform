package http

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

type CartHandler struct {
	dto.Unimplemented

	cartService cartService
	validator   *validator.Validate
}

type cartService interface {
	GetActiveCart(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
	AddCartItem(ctx context.Context, input cart.AddCartItemInput) (domain.Cart, error)
	UpdateCartItem(ctx context.Context, input cart.UpdateCartItemInput) (domain.Cart, error)
	RemoveCartItem(ctx context.Context, input cart.RemoveCartItemInput) (domain.Cart, error)
}

func NewCartHandler(cartService cartService) *CartHandler {
	return &CartHandler{
		cartService: cartService,
		validator:   validator.New(validator.WithRequiredStructEnabled()),
	}
}

func (h *CartHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *CartHandler) HandleOpenAPIError(w http.ResponseWriter, r *http.Request, _ error) {
	h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request parameters"))
}

func (h *CartHandler) GetActiveCart(w http.ResponseWriter, r *http.Request) {
	claims, ok := transport.ClaimsFromContext(r.Context())
	if !ok {
		h.writeError(w, r, commonerrors.Unauthorized("unauthorized", "unauthorized"))
		return
	}

	result, err := h.cartService.GetActiveCart(r.Context(), claims.UserID)
	if err != nil {
		h.writeError(w, r, mapCartError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, mapCartDTO(result))
}

func (h *CartHandler) AddCartItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := transport.ClaimsFromContext(r.Context())
	if !ok {
		h.writeError(w, r, commonerrors.Unauthorized("unauthorized", "unauthorized"))
		return
	}

	var request dto.AddCartItemRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}
	if err := h.validator.Struct(request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	result, err := h.cartService.AddCartItem(r.Context(), cart.AddCartItemInput{
		UserID:   claims.UserID,
		SKU:      request.Sku,
		Quantity: int64(request.Quantity),
	})
	if err != nil {
		h.writeError(w, r, mapCartError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, mapCartDTO(result))
}

func (h *CartHandler) UpdateCartItem(w http.ResponseWriter, r *http.Request, sku dto.Sku) {
	claims, ok := transport.ClaimsFromContext(r.Context())
	if !ok {
		h.writeError(w, r, commonerrors.Unauthorized("unauthorized", "unauthorized"))
		return
	}

	var request dto.UpdateCartItemRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}
	if err := h.validator.Struct(request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	result, err := h.cartService.UpdateCartItem(r.Context(), cart.UpdateCartItemInput{
		UserID:   claims.UserID,
		SKU:      sku,
		Quantity: int64(request.Quantity),
	})
	if err != nil {
		h.writeError(w, r, mapCartError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, mapCartDTO(result))
}

func (h *CartHandler) RemoveCartItem(w http.ResponseWriter, r *http.Request, sku dto.Sku) {
	claims, ok := transport.ClaimsFromContext(r.Context())
	if !ok {
		h.writeError(w, r, commonerrors.Unauthorized("unauthorized", "unauthorized"))
		return
	}

	result, err := h.cartService.RemoveCartItem(r.Context(), cart.RemoveCartItemInput{
		UserID: claims.UserID,
		SKU:    sku,
	})
	if err != nil {
		h.writeError(w, r, mapCartError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, mapCartDTO(result))
}

func (h *CartHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httpErr := commonerrors.FromError(err)
	commonerrors.WriteJSON(w, httpErr, transport.RequestIDFromContext(r.Context()))
}

func mapCartError(err error) error {
	switch {
	case errors.Is(err, cart.ErrInvalidUserID),
		errors.Is(err, cart.ErrInvalidSKU),
		errors.Is(err, cart.ErrInvalidQuantity):
		return commonerrors.BadRequest("invalid_request", "invalid cart input")
	case errors.Is(err, cart.ErrCartNotFound):
		return commonerrors.NotFound("cart_not_found", "cart not found")
	case errors.Is(err, cart.ErrCartItemNotFound):
		return commonerrors.NotFound("cart_item_not_found", "cart item not found")
	case errors.Is(err, cart.ErrProductSnapshotNotFound):
		return commonerrors.NotFound("product_not_found", "product not found")
	case errors.Is(err, cart.ErrCartItemAlreadyExists):
		return commonerrors.Conflict("cart_item_already_exists", "cart item already exists")
	case errors.Is(err, cart.ErrCartCurrencyMismatch):
		return commonerrors.Conflict("cart_currency_mismatch", "cart currency mismatch")
	default:
		return commonerrors.InternalError("internal_error")
	}
}

func mapCartDTO(cartResult domain.Cart) dto.Cart {
	items := make([]dto.CartItem, 0, len(cartResult.Items))
	for i := range cartResult.Items {
		item := cartResult.Items[i]

		itemName := item.Name
		itemCurrency := item.Currency

		items = append(items, dto.CartItem{
			Sku:       item.SKU,
			Quantity:  int(item.Quantity),
			UnitPrice: int(item.UnitPrice),
			LineTotal: int(item.LineTotal),
			Name:      &itemName,
			Currency:  &itemCurrency,
		})
	}

	return dto.Cart{
		CartId:      cartResult.ID.String(),
		UserId:      cartResult.UserID.String(),
		Status:      dto.CartStatus(cartResult.Status),
		Currency:    cartResult.Currency,
		Items:       items,
		TotalAmount: toInt(cartResult.TotalAmount),
	}
}

func toInt(value int64) int {
	if value > math.MaxInt {
		return math.MaxInt
	}

	if value < math.MinInt {
		return math.MinInt
	}

	return int(value)
}

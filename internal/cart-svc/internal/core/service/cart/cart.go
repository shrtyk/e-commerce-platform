package cart

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
)

type AddCartItemInput struct {
	UserID   uuid.UUID
	SKU      string
	Quantity int64
}

type UpdateCartItemInput struct {
	UserID   uuid.UUID
	SKU      string
	Quantity int64
}

type RemoveCartItemInput struct {
	UserID uuid.UUID
	SKU    string
}

func (s *CartService) GetActiveCart(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	if userID == uuid.Nil {
		return domain.Cart{}, ErrInvalidUserID
	}

	cart, err := s.carts.GetActiveByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			return domain.Cart{}, ErrCartNotFound
		}

		return domain.Cart{}, fmt.Errorf("get active cart: %w", err)
	}

	items, err := s.items.ListByCartID(ctx, cart.ID)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("list cart items: %w", err)
	}

	cart.Items = items

	recalculated, err := cart.RecalculateTotals()
	if err != nil {
		return domain.Cart{}, fmt.Errorf("recalculate cart totals: %w", err)
	}

	return recalculated, nil
}

func (s *CartService) AddCartItem(ctx context.Context, input AddCartItemInput) (domain.Cart, error) {
	if input.UserID == uuid.Nil {
		return domain.Cart{}, ErrInvalidUserID
	}

	sku := strings.TrimSpace(input.SKU)
	if sku == "" {
		return domain.Cart{}, ErrInvalidSKU
	}

	if input.Quantity <= 0 {
		return domain.Cart{}, ErrInvalidQuantity
	}

	snapshot, err := s.snapshots.GetBySKU(ctx, sku)
	if err != nil {
		if errors.Is(err, outbound.ErrProductSnapshotNotFound) {
			return domain.Cart{}, ErrProductSnapshotNotFound
		}

		return domain.Cart{}, fmt.Errorf("get product snapshot by sku: %w", err)
	}

	cart, err := s.ensureActiveCart(ctx, input.UserID, snapshot.Currency)
	if err != nil {
		return domain.Cart{}, err
	}

	if cart.Currency != snapshot.Currency {
		return domain.Cart{}, ErrCartCurrencyMismatch
	}

	_, err = s.items.Insert(ctx, outbound.CartItemInsertParams{
		CartID:      cart.ID,
		SKU:         snapshot.SKU,
		Quantity:    input.Quantity,
		UnitPrice:   snapshot.UnitPrice,
		Currency:    snapshot.Currency,
		ProductName: snapshot.Name,
	})
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			return domain.Cart{}, ErrCartNotFound
		}
		if errors.Is(err, outbound.ErrCartItemAlreadyExists) {
			return domain.Cart{}, ErrCartItemAlreadyExists
		}

		return domain.Cart{}, fmt.Errorf("insert cart item: %w", err)
	}

	return s.loadCartWithItems(ctx, cart)
}

func (s *CartService) UpdateCartItem(ctx context.Context, input UpdateCartItemInput) (domain.Cart, error) {
	if input.UserID == uuid.Nil {
		return domain.Cart{}, ErrInvalidUserID
	}

	sku := strings.TrimSpace(input.SKU)
	if sku == "" {
		return domain.Cart{}, ErrInvalidSKU
	}

	if input.Quantity <= 0 {
		return domain.Cart{}, ErrInvalidQuantity
	}

	cart, err := s.GetActiveCart(ctx, input.UserID)
	if err != nil {
		return domain.Cart{}, err
	}

	_, err = s.items.UpdateQuantity(ctx, cart.ID, sku, input.Quantity)
	if err != nil {
		if errors.Is(err, outbound.ErrCartItemNotFound) {
			return domain.Cart{}, ErrCartItemNotFound
		}

		return domain.Cart{}, fmt.Errorf("update cart item quantity: %w", err)
	}

	return s.GetActiveCart(ctx, input.UserID)
}

func (s *CartService) RemoveCartItem(ctx context.Context, input RemoveCartItemInput) (domain.Cart, error) {
	if input.UserID == uuid.Nil {
		return domain.Cart{}, ErrInvalidUserID
	}

	sku := strings.TrimSpace(input.SKU)
	if sku == "" {
		return domain.Cart{}, ErrInvalidSKU
	}

	cart, err := s.GetActiveCart(ctx, input.UserID)
	if err != nil {
		return domain.Cart{}, err
	}

	err = s.items.Delete(ctx, cart.ID, sku)
	if err != nil {
		if errors.Is(err, outbound.ErrCartItemNotFound) {
			return domain.Cart{}, ErrCartItemNotFound
		}

		return domain.Cart{}, fmt.Errorf("delete cart item: %w", err)
	}

	return s.GetActiveCart(ctx, input.UserID)
}

func (s *CartService) ensureActiveCart(ctx context.Context, userID uuid.UUID, currency string) (domain.Cart, error) {
	cart, err := s.carts.GetActiveByUserID(ctx, userID)
	if err == nil {
		return cart, nil
	}

	if !errors.Is(err, outbound.ErrCartNotFound) {
		return domain.Cart{}, fmt.Errorf("get active cart: %w", err)
	}

	created, createErr := s.carts.CreateActive(ctx, userID, currency)
	if createErr != nil {
		if errors.Is(createErr, outbound.ErrCartAlreadyExists) {
			cartAfterConflict, getErr := s.carts.GetActiveByUserID(ctx, userID)
			if getErr != nil {
				if errors.Is(getErr, outbound.ErrCartNotFound) {
					return domain.Cart{}, fmt.Errorf("get active cart after conflict: %w", createErr)
				}

				return domain.Cart{}, fmt.Errorf("get active cart after conflict: %w", getErr)
			}

			return cartAfterConflict, nil
		}

		return domain.Cart{}, fmt.Errorf("create active cart: %w", createErr)
	}

	return created, nil
}

func (s *CartService) loadCartWithItems(ctx context.Context, cart domain.Cart) (domain.Cart, error) {
	items, err := s.items.ListByCartID(ctx, cart.ID)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("list cart items: %w", err)
	}

	cart.Items = items

	recalculated, err := cart.RecalculateTotals()
	if err != nil {
		return domain.Cart{}, fmt.Errorf("recalculate cart totals: %w", err)
	}

	return recalculated, nil
}

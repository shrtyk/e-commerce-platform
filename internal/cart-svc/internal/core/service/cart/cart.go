package cart

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
		if !errors.Is(err, outbound.ErrProductSnapshotNotFound) {
			return domain.Cart{}, fmt.Errorf("get product snapshot by sku: %w", err)
		}

		snapshot, err = s.resolveProductSnapshotBySKU(ctx, sku)
		if err != nil {
			return domain.Cart{}, err
		}
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

func (s *CartService) resolveProductSnapshotBySKU(ctx context.Context, sku string) (domain.ProductSnapshot, error) {
	if s.catalog == nil {
		return domain.ProductSnapshot{}, ErrProductSnapshotNotFound
	}

	product, err := s.catalog.GetProductBySKU(ctx, sku)
	if err != nil {
		if errors.Is(err, outbound.ErrProductNotFound) {
			return domain.ProductSnapshot{}, ErrProductSnapshotNotFound
		}

		return domain.ProductSnapshot{}, fmt.Errorf("get product by sku from catalog: %w", err)
	}

	if !product.IsPublished {
		return domain.ProductSnapshot{}, ErrProductSnapshotNotFound
	}

	productID, err := toProductSnapshotProductID(product.ProductID)
	if err != nil {
		return domain.ProductSnapshot{}, fmt.Errorf("parse catalog product id: %w", err)
	}

	now := time.Now().UTC()
	snapshot, err := domain.NewProductSnapshot(product.SKU, productID, product.Name, product.Price, product.Currency, now, now)
	if err != nil {
		return domain.ProductSnapshot{}, fmt.Errorf("create product snapshot: %w", err)
	}

	upserted, err := s.snapshots.Upsert(ctx, snapshot)
	if err != nil {
		return domain.ProductSnapshot{}, fmt.Errorf("upsert product snapshot: %w", err)
	}

	return upserted, nil
}

func toProductSnapshotProductID(raw string) (*uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parsedID, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, err
	}

	if parsedID == uuid.Nil {
		return nil, ErrInvalidCatalogProductID
	}

	return &parsedID, nil
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

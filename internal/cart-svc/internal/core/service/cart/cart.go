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

	if s.cache != nil {
		cachedCart, found, cacheErr := s.cache.GetActiveByUserID(ctx, userID)
		if cacheErr == nil && found {
			if validatedCachedCart, ok := validateCachedActiveCart(userID, cachedCart); ok {
				return validatedCachedCart, nil
			}
		}
	}

	cart, err := s.loadActiveCartFromStorage(ctx, userID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			emptyCart := s.newEmptyActiveCart(userID)
			s.setActiveCartCache(ctx, userID, emptyCart)

			return emptyCart, nil
		}

		return domain.Cart{}, err
	}

	s.setActiveCartCache(ctx, userID, cart)

	return cart, nil
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

	authoritativeCart, err := s.loadCartWithItems(ctx, cart)
	if err != nil {
		return domain.Cart{}, err
	}

	s.setActiveCartCache(ctx, input.UserID, authoritativeCart)

	return authoritativeCart, nil
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

	cart, err := s.loadActiveCartFromStorage(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			return domain.Cart{}, ErrCartNotFound
		}

		return domain.Cart{}, err
	}

	_, err = s.items.UpdateQuantity(ctx, cart.ID, sku, input.Quantity)
	if err != nil {
		if errors.Is(err, outbound.ErrCartItemNotFound) {
			return domain.Cart{}, ErrCartItemNotFound
		}

		return domain.Cart{}, fmt.Errorf("update cart item quantity: %w", err)
	}

	authoritativeCart, err := s.loadActiveCartFromStorage(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			return domain.Cart{}, ErrCartNotFound
		}

		return domain.Cart{}, err
	}

	s.setActiveCartCache(ctx, input.UserID, authoritativeCart)

	return authoritativeCart, nil
}

func (s *CartService) RemoveCartItem(ctx context.Context, input RemoveCartItemInput) (domain.Cart, error) {
	if input.UserID == uuid.Nil {
		return domain.Cart{}, ErrInvalidUserID
	}

	sku := strings.TrimSpace(input.SKU)
	if sku == "" {
		return domain.Cart{}, ErrInvalidSKU
	}

	cart, err := s.loadActiveCartFromStorage(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			return domain.Cart{}, ErrCartNotFound
		}

		return domain.Cart{}, err
	}

	err = s.items.Delete(ctx, cart.ID, sku)
	if err != nil {
		if errors.Is(err, outbound.ErrCartItemNotFound) {
			return domain.Cart{}, ErrCartItemNotFound
		}

		return domain.Cart{}, fmt.Errorf("delete cart item: %w", err)
	}

	authoritativeCart, err := s.loadActiveCartFromStorage(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			authoritativeCart = s.newEmptyActiveCart(input.UserID)
			s.setActiveCartCache(ctx, input.UserID, authoritativeCart)

			return authoritativeCart, nil
		}

		return domain.Cart{}, err
	}

	s.setActiveCartCache(ctx, input.UserID, authoritativeCart)

	return authoritativeCart, nil
}

func (s *CartService) GetCheckoutSnapshot(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	if userID == uuid.Nil {
		return domain.Cart{}, ErrInvalidUserID
	}

	cart, err := s.loadActiveCartFromStorage(ctx, userID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			return s.newEmptyActiveCart(userID), nil
		}

		return domain.Cart{}, err
	}

	repriced, err := s.repriceCheckoutSnapshot(ctx, cart)
	if err != nil {
		return domain.Cart{}, err
	}

	return repriced, nil
}

func (s *CartService) repriceCheckoutSnapshot(ctx context.Context, stored domain.Cart) (domain.Cart, error) {
	if s.catalog == nil {
		return domain.Cart{}, fmt.Errorf("catalog reader is not configured")
	}

	repriced := stored
	repriced.Items = make([]domain.CartItem, 0, len(stored.Items))

	for i := range stored.Items {
		storedItem := stored.Items[i]

		product, err := s.catalog.GetProductBySKU(ctx, storedItem.SKU)
		if err != nil {
			if errors.Is(err, outbound.ErrProductNotFound) {
				return domain.Cart{}, ErrProductSnapshotNotFound
			}

			return domain.Cart{}, fmt.Errorf("get product by sku from catalog: %w", err)
		}

		if !product.IsPublished {
			return domain.Cart{}, ErrProductSnapshotNotFound
		}

		repricedItem, err := domain.NewCartItem(
			storedItem.SKU,
			product.Name,
			storedItem.Quantity,
			product.Price,
			product.Currency,
			storedItem.CreatedAt,
			storedItem.UpdatedAt,
		)
		if err != nil {
			return domain.Cart{}, fmt.Errorf("create repriced cart item: %w", err)
		}

		repriced.Items = append(repriced.Items, repricedItem)
	}

	if len(repriced.Items) > 0 {
		repriced.Currency = repriced.Items[0].Currency
	}

	recalculated, err := repriced.RecalculateTotals()
	if err != nil {
		return domain.Cart{}, fmt.Errorf("recalculate checkout snapshot totals: %w", err)
	}

	return recalculated, nil
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

func (s *CartService) loadActiveCartFromStorage(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	cart, err := s.carts.GetActiveByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, outbound.ErrCartNotFound) {
			return domain.Cart{}, outbound.ErrCartNotFound
		}

		return domain.Cart{}, fmt.Errorf("get active cart: %w", err)
	}

	loaded, err := s.loadCartWithItems(ctx, cart)
	if err != nil {
		return domain.Cart{}, err
	}

	return loaded, nil
}

func (s *CartService) setActiveCartCache(ctx context.Context, userID uuid.UUID, cart domain.Cart) {
	if s.cache == nil {
		return
	}

	if err := s.cache.SetActiveByUserID(ctx, userID, cart, s.cacheTTL); err != nil {
		_ = s.cache.DeleteActiveByUserID(ctx, userID)
	}
}

func validateCachedActiveCart(userID uuid.UUID, cart domain.Cart) (domain.Cart, bool) {
	if cart.UserID != userID {
		return domain.Cart{}, false
	}

	if cart.Status != domain.CartStatusActive {
		return domain.Cart{}, false
	}

	recalculated, err := cart.RecalculateTotals()
	if err != nil {
		return domain.Cart{}, false
	}

	return recalculated, true
}

func (s *CartService) newEmptyActiveCart(userID uuid.UUID) domain.Cart {
	return domain.Cart{
		UserID:      userID,
		Status:      domain.CartStatusActive,
		Items:       []domain.CartItem{},
		TotalAmount: 0,
	}
}

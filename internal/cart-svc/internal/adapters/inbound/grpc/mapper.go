package grpc

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
)

func toUserID(raw string) (uuid.UUID, error) {
	userID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse user id: %w", err)
	}

	if userID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("parse user id: user id must not be zero value")
	}

	return userID, nil
}

func toAddCartItemInput(userID uuid.UUID, req *cartv1.AddCartItemRequest) cart.AddCartItemInput {
	if req == nil {
		return cart.AddCartItemInput{UserID: userID}
	}

	return cart.AddCartItemInput{
		UserID:   userID,
		SKU:      req.GetSku(),
		Quantity: req.GetQuantity(),
	}
}

func toUpdateCartItemInput(userID uuid.UUID, req *cartv1.UpdateCartItemRequest) cart.UpdateCartItemInput {
	if req == nil {
		return cart.UpdateCartItemInput{UserID: userID}
	}

	return cart.UpdateCartItemInput{
		UserID:   userID,
		SKU:      req.GetSku(),
		Quantity: req.GetQuantity(),
	}
}

func toRemoveCartItemInput(userID uuid.UUID, req *cartv1.RemoveCartItemRequest) cart.RemoveCartItemInput {
	if req == nil {
		return cart.RemoveCartItemInput{UserID: userID}
	}

	return cart.RemoveCartItemInput{
		UserID: userID,
		SKU:    req.GetSku(),
	}
}

func toGetActiveCartResponse(result domain.Cart) *cartv1.GetActiveCartResponse {
	return &cartv1.GetActiveCartResponse{Cart: toProtoCart(result)}
}

func toAddCartItemResponse(result domain.Cart) *cartv1.AddCartItemResponse {
	return &cartv1.AddCartItemResponse{Cart: toProtoCart(result)}
}

func toUpdateCartItemResponse(result domain.Cart) *cartv1.UpdateCartItemResponse {
	return &cartv1.UpdateCartItemResponse{Cart: toProtoCart(result)}
}

func toRemoveCartItemResponse(result domain.Cart) *cartv1.RemoveCartItemResponse {
	return &cartv1.RemoveCartItemResponse{Cart: toProtoCart(result)}
}

func toProtoCart(result domain.Cart) *cartv1.Cart {
	items := make([]*cartv1.CartItem, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toProtoCartItem(result.Items[i]))
	}

	return &cartv1.Cart{
		CartId:      result.ID.String(),
		UserId:      result.UserID.String(),
		Status:      toProtoCartStatus(result.Status),
		Currency:    result.Currency,
		Items:       items,
		TotalAmount: toMoney(result.TotalAmount, result.Currency),
	}
}

func toProtoCartItem(item domain.CartItem) *cartv1.CartItem {
	return &cartv1.CartItem{
		Sku:       item.SKU,
		Name:      item.Name,
		Quantity:  item.Quantity,
		UnitPrice: toMoney(item.UnitPrice, item.Currency),
		LineTotal: toMoney(item.LineTotal, item.Currency),
	}
}

func toMoney(amount int64, currency string) *commonv1.Money {
	return &commonv1.Money{Amount: amount, Currency: currency}
}

func toProtoCartStatus(status domain.CartStatus) cartv1.CartStatus {
	switch status {
	case domain.CartStatusActive:
		return cartv1.CartStatus_CART_STATUS_ACTIVE
	case domain.CartStatusCheckedOut:
		return cartv1.CartStatus_CART_STATUS_CHECKED_OUT
	case domain.CartStatusExpired:
		return cartv1.CartStatus_CART_STATUS_EXPIRED
	default:
		return cartv1.CartStatus_CART_STATUS_UNSPECIFIED
	}
}

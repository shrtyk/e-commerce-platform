//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/testhelper"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
)

func TestHTTPAndGRPCCartFlowWithSnapshotFallbackAndRedisCache(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
	testhelper.CleanupRedis(t, testhelper.TestRedis)

	stack := newCartStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	sku := "SKU-INT-FALLBACK"
	productID := uuid.NewString()

	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: productID,
		SKU:       sku,
		Name:      "Integration Product",
		Price:     1499,
		Currency:  "USD",
		Status:    catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED,
	})

	addHTTP := addItemHTTP(t, stack, token, dto.AddCartItemRequest{Sku: sku, Quantity: 2}, http.StatusOK)
	require.Equal(t, userID.String(), addHTTP.UserId)
	require.Equal(t, "USD", addHTTP.Currency)
	require.Equal(t, 2998, addHTTP.TotalAmount)
	require.Len(t, addHTTP.Items, 1)
	require.Equal(t, sku, addHTTP.Items[0].Sku)
	require.Equal(t, 2, addHTTP.Items[0].Quantity)

	snapshot := readSnapshotRow(t, stack.DB, sku)
	require.Equal(t, sku, snapshot.SKU)
	require.Equal(t, "Integration Product", snapshot.Name)
	require.Equal(t, int64(1499), snapshot.UnitPrice)
	require.Equal(t, "USD", snapshot.Currency)
	require.NotNil(t, snapshot.ProductID)
	require.Equal(t, productID, snapshot.ProductID.String())

	cacheKey := redisActiveCartKey(userID)
	require.NoError(t, stack.RedisClient.Del(context.Background(), cacheKey).Err())

	grpcClient := cartv1.NewCartServiceClient(stack.GRPCConn)
	grpcCtx := grpcAuthContext(token)

	grpcAdd, err := grpcClient.AddCartItem(grpcCtx, &cartv1.AddCartItemRequest{
		UserId:   userID.String(),
		Sku:      sku,
		Quantity: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), grpcAdd.GetCart().GetItems()[0].GetQuantity())
	require.Equal(t, int64(4497), grpcAdd.GetCart().GetTotalAmount().GetAmount())

	fallbackSnapshot := readSnapshotRow(t, stack.DB, sku)
	require.Equal(t, sku, fallbackSnapshot.SKU)
	require.Equal(t, int64(1499), fallbackSnapshot.UnitPrice)

	cachePayload, err := stack.RedisClient.Get(context.Background(), cacheKey).Bytes()
	require.NoError(t, err)
	require.NotEmpty(t, cachePayload)

	grpcGet, err := grpcClient.GetActiveCart(grpcCtx, &cartv1.GetActiveCartRequest{UserId: userID.String()})
	require.NoError(t, err)
	require.Equal(t, int64(4497), grpcGet.GetCart().GetTotalAmount().GetAmount())
	require.Equal(t, int64(3), grpcGet.GetCart().GetItems()[0].GetQuantity())
}

func TestCheckoutSnapshotRepricesWithoutMutatingStoredCart(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
	testhelper.CleanupRedis(t, testhelper.TestRedis)

	stack := newCartStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	sku := "SKU-INT-REPRICE"

	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       sku,
		Name:      "Original Name",
		Price:     1000,
		Currency:  "USD",
		Status:    catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED,
	})

	added := addItemHTTP(t, stack, token, dto.AddCartItemRequest{Sku: sku, Quantity: 2}, http.StatusOK)
	require.Equal(t, 2000, added.TotalAmount)
	require.Equal(t, "Original Name", stringPtrValue(added.Items[0].Name))
	require.Equal(t, 1000, added.Items[0].UnitPrice)

	storedBefore := readStoredCartItem(t, stack.DB, userID, sku)
	require.Equal(t, int64(1000), storedBefore.UnitPrice)
	require.Equal(t, "Original Name", storedBefore.ProductName)
	require.NotNil(t, storedBefore.ProductID)

	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: storedBefore.ProductID.String(),
		SKU:       sku,
		Name:      "Repriced Name",
		Price:     1250,
		Currency:  "USD",
		Status:    catalogv1.ProductStatus_PRODUCT_STATUS_PUBLISHED,
	})

	grpcClient := cartv1.NewCartServiceClient(stack.GRPCConn)
	snapshotRes, err := grpcClient.GetCheckoutSnapshot(
		grpcAuthContext(token),
		&cartv1.GetCheckoutSnapshotRequest{UserId: userID.String()},
	)
	require.NoError(t, err)

	require.Equal(t, userID.String(), snapshotRes.GetSnapshot().GetUserId())
	require.Equal(t, "USD", snapshotRes.GetSnapshot().GetCurrency())
	require.Equal(t, int64(2500), snapshotRes.GetSnapshot().GetTotalAmount().GetAmount())
	require.Len(t, snapshotRes.GetSnapshot().GetItems(), 1)
	require.Equal(t, sku, snapshotRes.GetSnapshot().GetItems()[0].GetSku())
	require.Equal(t, "Repriced Name", snapshotRes.GetSnapshot().GetItems()[0].GetName())
	require.Equal(t, int64(1250), snapshotRes.GetSnapshot().GetItems()[0].GetUnitPrice().GetAmount())
	require.Equal(t, int64(2500), snapshotRes.GetSnapshot().GetItems()[0].GetLineTotal().GetAmount())

	storedAfter := readStoredCartItem(t, stack.DB, userID, sku)
	require.Equal(t, int64(1000), storedAfter.UnitPrice)
	require.Equal(t, "Original Name", storedAfter.ProductName)
	require.Equal(t, int64(2), storedAfter.Quantity)

	grpcGet, err := grpcClient.GetActiveCart(
		grpcAuthContext(token),
		&cartv1.GetActiveCartRequest{UserId: userID.String()},
	)
	require.NoError(t, err)
	require.Equal(t, int64(2000), grpcGet.GetCart().GetTotalAmount().GetAmount())
	require.Equal(t, int64(1000), grpcGet.GetCart().GetItems()[0].GetUnitPrice().GetAmount())
	require.Equal(t, "Original Name", grpcGet.GetCart().GetItems()[0].GetName())

	_, err = grpcClient.GetCheckoutSnapshot(
		grpcAuthContext(token),
		&cartv1.GetCheckoutSnapshotRequest{UserId: uuid.NewString()},
	)
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestHTTPAuthRejectsMissingAndMalformedBearerToken(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
	testhelper.CleanupRedis(t, testhelper.TestRedis)

	stack := newCartStack(t)

	body := dto.AddCartItemRequest{Sku: "SKU-AUTH-HTTP", Quantity: 1}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		authorization string
	}{
		{name: "missing token", authorization: ""},
		{name: "malformed token", authorization: "Bearer not-a-jwt"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/cart/items", bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			if tc.authorization != "" {
				request.Header.Set("Authorization", tc.authorization)
			}

			response := httptest.NewRecorder()
			stack.HTTPHandler.ServeHTTP(response, request)

			require.Equal(t, http.StatusUnauthorized, response.Code)
		})
	}
}

func TestGRPCAuthRejectsMissingMalformedAndMismatchedUser(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
	testhelper.CleanupRedis(t, testhelper.TestRedis)

	stack := newCartStack(t)
	client := cartv1.NewCartServiceClient(stack.GRPCConn)

	tokenUserID := uuid.New()
	validToken := testhelper.MintAccessToken(t, tokenUserID)

	testCases := []struct {
		name       string
		ctx        context.Context
		request    *cartv1.AddCartItemRequest
		expectCode codes.Code
	}{
		{
			name: "missing token",
			ctx:  context.Background(),
			request: &cartv1.AddCartItemRequest{
				UserId:   tokenUserID.String(),
				Sku:      "SKU-AUTH-GRPC",
				Quantity: 1,
			},
			expectCode: codes.Unauthenticated,
		},
		{
			name: "malformed token",
			ctx:  grpcAuthContext("not-a-jwt"),
			request: &cartv1.AddCartItemRequest{
				UserId:   tokenUserID.String(),
				Sku:      "SKU-AUTH-GRPC",
				Quantity: 1,
			},
			expectCode: codes.Unauthenticated,
		},
		{
			name: "request user mismatch",
			ctx:  grpcAuthContext(validToken),
			request: &cartv1.AddCartItemRequest{
				UserId:   uuid.NewString(),
				Sku:      "SKU-AUTH-GRPC",
				Quantity: 1,
			},
			expectCode: codes.PermissionDenied,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.AddCartItem(tc.ctx, tc.request)
			require.Error(t, err)
			require.Equal(t, tc.expectCode, status.Code(err))
		})
	}
}

type snapshotRow struct {
	SKU       string
	ProductID *uuid.UUID
	Name      string
	UnitPrice int64
	Currency  string
}

type storedCartItemRow struct {
	SKU         string
	Quantity    int64
	UnitPrice   int64
	ProductName string
	ProductID   *uuid.UUID
}

func newCartStack(t *testing.T) *testhelper.TestStack {
	t.Helper()

	return testhelper.NewTestStack(t, testhelper.TestDB, testhelper.TestRedis)
}

func addItemHTTP(
	t *testing.T,
	stack *testhelper.TestStack,
	token string,
	body dto.AddCartItemRequest,
	expectedStatus int,
) dto.Cart {
	t.Helper()

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/cart/items", bytes.NewReader(payload))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, expectedStatus, response.Code)

	if expectedStatus != http.StatusOK {
		return dto.Cart{}
	}

	var result dto.Cart
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))

	return result
}

func grpcAuthContext(accessToken string) context.Context {
	return metadata.NewOutgoingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer "+accessToken),
	)
}

func redisActiveCartKey(userID uuid.UUID) string {
	return "cart:active:user:" + userID.String()
}

func readSnapshotRow(t *testing.T, db *sql.DB, sku string) snapshotRow {
	t.Helper()

	var row snapshotRow
	var productID uuid.NullUUID

	err := db.QueryRowContext(
		context.Background(),
		`SELECT sku, product_id, name, unit_price, currency FROM product_snapshots WHERE sku = $1`,
		sku,
	).Scan(&row.SKU, &productID, &row.Name, &row.UnitPrice, &row.Currency)
	require.NoError(t, err)

	if productID.Valid {
		value := productID.UUID
		row.ProductID = &value
	}

	return row
}

func readStoredCartItem(t *testing.T, db *sql.DB, userID uuid.UUID, sku string) storedCartItemRow {
	t.Helper()

	var row storedCartItemRow
	var productID uuid.NullUUID

	err := db.QueryRowContext(
		context.Background(),
		`SELECT ci.sku, ci.quantity, ci.unit_price, ci.product_name, ps.product_id
		 FROM cart_items ci
		 JOIN carts c ON c.cart_id = ci.cart_id
		 JOIN product_snapshots ps ON ps.sku = ci.sku
		 WHERE c.user_id = $1 AND c.status = 'active' AND ci.sku = $2`,
		userID,
		sku,
	).Scan(&row.SKU, &row.Quantity, &row.UnitPrice, &row.ProductName, &productID)
	require.NoError(t, err)

	if productID.Valid {
		value := productID.UUID
		row.ProductID = &value
	}

	return row
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

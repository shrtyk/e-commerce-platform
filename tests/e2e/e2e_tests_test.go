package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckoutHappyPathThroughOrderConfirmation(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(
		t,
		readinessGateway,
		readinessIdentity,
		readinessProduct,
		readinessCart,
		readinessOrder,
	)

	adminToken := harness.loginAdmin(t)
	products := harness.createPublishedProducts(t, adminToken)

	shopperToken := harness.registerShopper(t)
	harness.assertPublishedCatalogContainsCreatedProduct(t, products)

	harness.addItemToCart(t, shopperToken, products[0].SKU, 2)
	order := harness.checkout(t, shopperToken)
	harness.waitForOrderConfirmed(t, shopperToken, order.OrderID)
}

func TestCheckoutPaymentFailureCancelsOrderAndReleasesStock(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(
		t,
		readinessGateway,
		readinessIdentity,
		readinessProduct,
		readinessCart,
		readinessOrder,
	)

	adminToken := harness.loginAdmin(t)
	product := harness.createPublishedProduct(t, adminToken, createProductInput{
		Price:           1001,
		InitialQuantity: 2,
	})

	firstShopperToken := harness.registerShopper(t)
	harness.assertPublishedCatalogContainsCreatedProduct(t, []createdProduct{product})
	harness.addItemToCart(t, firstShopperToken, product.SKU, 1)
	checkoutErr := harness.checkoutExpectError(t, firstShopperToken, http.StatusConflict)
	require.Equal(t, "PAYMENT_DECLINED", checkoutErr.Code)

	secondShopperToken := harness.registerShopper(t)
	harness.addItemToCart(t, secondShopperToken, product.SKU, 2)
	succeededOrder := harness.checkout(t, secondShopperToken)
	harness.waitForOrderConfirmed(t, secondShopperToken, succeededOrder.OrderID)
}

func TestCheckoutRejectsEmptyCart(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessIdentity, readinessCart, readinessOrder)

	shopperToken := harness.registerShopper(t)

	checkoutErr := harness.checkoutExpectError(t, shopperToken, http.StatusConflict)
	require.Equal(t, "CART_EMPTY", checkoutErr.Code)
}

func TestAddToCartRejectsUnknownSKU(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessIdentity, readinessProduct, readinessCart)

	shopperToken := harness.registerShopper(t)

	errResponse := harness.addItemToCartExpectError(t, shopperToken, harness.newUnknownSKU(), 1, 404)
	require.Equal(t, "product_not_found", errResponse.Code)
}

func TestCheckoutIdempotencyKeyReplaysSameOrderIDForSamePayload(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessIdentity, readinessProduct, readinessCart, readinessOrder)

	adminToken := harness.loginAdmin(t)
	product := harness.createPublishedProduct(t, adminToken, createProductInput{
		Price:           2000,
		InitialQuantity: 10,
	})

	shopperToken := harness.registerShopper(t)
	harness.addItemToCart(t, shopperToken, product.SKU, 1)

	idempotencyKey := harness.newIdempotencyKey()
	firstOrder := harness.checkoutWithIdempotencyKeyExpectCode(t, shopperToken, idempotencyKey, http.StatusAccepted)
	secondOrder := harness.checkoutWithIdempotencyKeyExpectCode(t, shopperToken, idempotencyKey, http.StatusAccepted)

	require.Equal(t, firstOrder.OrderID, secondOrder.OrderID)
}

func TestCheckoutIdempotencyKeyRejectsDifferentPayload(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessIdentity, readinessProduct, readinessCart, readinessOrder)

	adminToken := harness.loginAdmin(t)
	products := harness.createPublishedProducts(t, adminToken)

	shopperToken := harness.registerShopper(t)
	harness.addItemToCart(t, shopperToken, products[0].SKU, 1)

	idempotencyKey := harness.newIdempotencyKey()
	firstOrder := harness.checkoutWithIdempotencyKeyExpectCode(t, shopperToken, idempotencyKey, http.StatusAccepted)
	require.NotEmpty(t, firstOrder.OrderID)

	checkoutErr := harness.checkoutWithPayloadAndIdempotencyKeyExpectError(t, shopperToken, map[string]any{"paymentMethod": "bank_transfer"}, idempotencyKey, http.StatusConflict)
	require.Equal(t, "IDEMPOTENCY_KEY_REUSED_WITH_DIFFERENT_PAYLOAD", checkoutErr.Code)
}

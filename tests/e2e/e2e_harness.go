package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	defaultE2EGatewayBaseURL = "http://localhost:8080"

	defaultAdminEmail    = "admin@example.com"
	defaultAdminPassword = "secret123"

	defaultShopperPassword = "secret123"

	defaultReadyTimeout     = 45 * time.Second
	defaultOrderWaitTimeout = 45 * time.Second
	defaultPollInterval     = 1 * time.Second

	defaultTransient429RetryMaxWait  = 2 * time.Second
	defaultTransient429RetryInterval = 100 * time.Millisecond
)

const (
	envTransient429RetryMaxWait  = "E2E_TRANSIENT_429_RETRY_MAX_WAIT"
	envTransient429RetryInterval = "E2E_TRANSIENT_429_RETRY_INTERVAL"
)

type readinessService string

const (
	readinessGateway  readinessService = "gateway"
	readinessIdentity readinessService = "identity"
	readinessProduct  readinessService = "product"
	readinessCart     readinessService = "cart"
	readinessOrder    readinessService = "order"
)

var supportedReadinessServices = []readinessService{
	readinessGateway,
	readinessIdentity,
	readinessProduct,
	readinessCart,
	readinessOrder,
}

type e2eHarness struct {
	gatewayBaseURL string

	adminEmail    string
	adminPassword string

	runID string

	readyTimeout     time.Duration
	orderWaitTimeout time.Duration
	pollInterval     time.Duration

	httpClient *http.Client
}

type authTokensResponse struct {
	AccessToken string `json:"accessToken"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type productWriteResponse struct {
	Product struct {
		ProductID string `json:"productId"`
		SKU       string `json:"sku"`
	} `json:"product"`
}

type productListResponse struct {
	Items []productItem `json:"items"`
}

type productItem struct {
	ProductID string `json:"productId"`
	SKU       string `json:"sku"`
}

type cartResponse struct {
	Items []struct {
		SKU      string `json:"sku"`
		Quantity int    `json:"quantity"`
	} `json:"items"`
}

type orderResponse struct {
	OrderID string `json:"orderId"`
	Status  string `json:"status"`
}

type createdProduct struct {
	ProductID string
	SKU       string
}

type createProductInput struct {
	Price           int
	InitialQuantity int
}

func newE2EHarness(t *testing.T) e2eHarness {
	t.Helper()

	h := e2eHarness{
		gatewayBaseURL:   envOrDefault("E2E_GATEWAY_BASE_URL", defaultE2EGatewayBaseURL),
		adminEmail:       envOrDefault("E2E_BOOTSTRAP_ADMIN_EMAIL", defaultAdminEmail),
		adminPassword:    envOrDefault("E2E_BOOTSTRAP_ADMIN_PASSWORD", defaultAdminPassword),
		readyTimeout:     durationEnvOrDefault(t, "E2E_READY_TIMEOUT", defaultReadyTimeout),
		orderWaitTimeout: durationEnvOrDefault(t, "E2E_ORDER_WAIT_TIMEOUT", defaultOrderWaitTimeout),
		pollInterval:     durationEnvOrDefault(t, "E2E_POLL_INTERVAL", defaultPollInterval),
		runID:            fmt.Sprintf("%d-%s", time.Now().UnixNano(), strings.ToLower(uuid.NewString()[:8])),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	require.Greater(t, h.pollInterval, time.Duration(0), "E2E_POLL_INTERVAL must be greater than 0")

	validateBaseURL(t, h.gatewayBaseURL, "E2E_GATEWAY_BASE_URL")

	return h
}

func (h e2eHarness) assertServicesReady(t *testing.T, services ...readinessService) {
	t.Helper()

	seen := map[readinessService]struct{}{}

	for _, service := range services {
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}

		spec := h.readinessServiceSpec(t, service)
		h.assertReadyEndpoint(t, spec)
	}
}

type readinessSpec struct {
	name         string
	path         string
	wantStatuses []int
}

func (h e2eHarness) readinessServiceSpec(t *testing.T, service readinessService) readinessSpec {
	t.Helper()

	switch service {
	case readinessGateway:
		return readinessSpec{name: "gateway", path: "/healthz", wantStatuses: []int{http.StatusOK}}
	case readinessIdentity:
		return readinessSpec{name: "identity-svc", path: "/v1/profile/me", wantStatuses: []int{http.StatusUnauthorized, http.StatusForbidden}}
	case readinessProduct:
		return readinessSpec{name: "product-svc", path: "/v1/products", wantStatuses: []int{http.StatusOK}}
	case readinessCart:
		return readinessSpec{name: "cart-svc", path: "/v1/cart", wantStatuses: []int{http.StatusUnauthorized, http.StatusForbidden}}
	case readinessOrder:
		return readinessSpec{name: "order-svc", path: "/v1/orders", wantStatuses: []int{http.StatusMethodNotAllowed, http.StatusUnauthorized, http.StatusForbidden}}
	default:
		require.FailNowf(t, "unknown readiness service", "service=%q", service)
		return readinessSpec{}
	}
}

func (h e2eHarness) loginAdmin(t *testing.T) string {
	t.Helper()

	response := doJSON[authTokensResponse](t, h.httpClient, requestSpec{
		Method:   http.MethodPost,
		URL:      h.gatewayBaseURL + "/v1/auth/login",
		Body:     map[string]any{"email": h.adminEmail, "password": h.adminPassword},
		WantCode: http.StatusOK,
	})

	require.NotEmpty(t, response.AccessToken, "admin accessToken must be returned")

	return response.AccessToken
}

func (h e2eHarness) createPublishedProducts(t *testing.T, adminToken string) []createdProduct {
	t.Helper()

	products := make([]createdProduct, 0, 2)

	for _, price := range []int{2000, 2400} {
		products = append(products, h.createPublishedProduct(t, adminToken, createProductInput{
			Price:           price,
			InitialQuantity: 10,
		}))
	}

	return products
}

func (h e2eHarness) createPublishedProduct(t *testing.T, adminToken string, input createProductInput) createdProduct {
	t.Helper()

	sku := h.newSKU("E2E")

	response := doJSON[productWriteResponse](t, h.httpClient, requestSpec{
		Method: http.MethodPost,
		URL:    h.gatewayBaseURL + "/v1/products",
		Auth:   adminToken,
		Body: map[string]any{
			"sku":             sku,
			"name":            "E2E product",
			"description":     "E2E product",
			"price":           input.Price,
			"currencyCode":    "USD",
			"status":          "published",
			"initialQuantity": input.InitialQuantity,
		},
		WantCode: http.StatusCreated,
	})

	require.NotEmpty(t, response.Product.ProductID, "productId must be returned")
	require.Equal(t, sku, response.Product.SKU)

	return createdProduct{
		ProductID: response.Product.ProductID,
		SKU:       response.Product.SKU,
	}
}

func (h e2eHarness) newUnknownSKU() string {
	return h.newSKU("E2E-UNKNOWN")
}

func (h e2eHarness) newSKU(prefix string) string {
	return fmt.Sprintf("%s-%s-%s", prefix, h.runID, strings.ToLower(uuid.NewString()[:8]))
}

func (h e2eHarness) registerShopper(t *testing.T) string {
	t.Helper()

	email := fmt.Sprintf("shopper-%s-%s@example.com", h.runID, strings.ToLower(uuid.NewString()[:8]))

	response := doJSON[authTokensResponse](t, h.httpClient, requestSpec{
		Method: http.MethodPost,
		URL:    h.gatewayBaseURL + "/v1/auth/register",
		Body: map[string]any{
			"email":       email,
			"password":    envOrDefault("E2E_SHOPPER_PASSWORD", defaultShopperPassword),
			"displayName": "Shopper",
		},
		WantCode: http.StatusCreated,
	})

	require.NotEmpty(t, response.AccessToken, "shopper accessToken must be returned")

	return response.AccessToken
}

func (h e2eHarness) assertPublishedCatalogContainsCreatedProduct(t *testing.T, created []createdProduct) {
	t.Helper()

	response := doJSON[productListResponse](t, h.httpClient, requestSpec{
		Method:   http.MethodGet,
		URL:      h.gatewayBaseURL + "/v1/products",
		WantCode: http.StatusOK,
	})

	require.NotEmpty(t, response.Items, "published product list must not be empty")

	for _, expected := range created {
		found := false
		for _, listed := range response.Items {
			if listed.SKU == expected.SKU {
				found = true
				break
			}
		}

		require.True(t, found, "published list must include created sku %q", expected.SKU)
	}
}

func (h e2eHarness) addItemToCart(t *testing.T, shopperToken, sku string, quantity int) {
	t.Helper()

	response := doJSON[cartResponse](t, h.httpClient, requestSpec{
		Method: http.MethodPost,
		URL:    h.gatewayBaseURL + "/v1/cart/items",
		Auth:   shopperToken,
		Body: map[string]any{
			"sku":      sku,
			"quantity": quantity,
		},
		WantCode: http.StatusOK,
	})

	match := false
	for _, item := range response.Items {
		if item.SKU == sku && item.Quantity == quantity {
			match = true
			break
		}
	}

	require.True(t, match, "cart must include added item sku=%q qty=%d", sku, quantity)
}

func (h e2eHarness) addItemToCartExpectError(t *testing.T, shopperToken, sku string, quantity, wantCode int) errorResponse {
	t.Helper()

	return doJSON[errorResponse](t, h.httpClient, requestSpec{
		Method: http.MethodPost,
		URL:    h.gatewayBaseURL + "/v1/cart/items",
		Auth:   shopperToken,
		Body: map[string]any{
			"sku":      sku,
			"quantity": quantity,
		},
		WantCode: wantCode,
	})
}

func (h e2eHarness) newIdempotencyKey() string {
	return fmt.Sprintf("%s-%s", h.runID, strings.ToLower(uuid.NewString()[:8]))
}

func (h e2eHarness) checkout(t *testing.T, shopperToken string) orderResponse {
	t.Helper()

	return h.checkoutExpectCode(t, shopperToken, http.StatusAccepted)
}

func (h e2eHarness) checkoutExpectError(t *testing.T, shopperToken string, wantCode int) errorResponse {
	t.Helper()

	return h.checkoutErrorExpectCode(t, shopperToken, wantCode)
}

func (h e2eHarness) checkoutExpectCode(t *testing.T, shopperToken string, wantCode int) orderResponse {
	t.Helper()

	return h.checkoutWithIdempotencyKeyExpectCode(t, shopperToken, h.newIdempotencyKey(), wantCode)
}

func (h e2eHarness) checkoutWithIdempotencyKeyExpectCode(t *testing.T, shopperToken, idempotencyKey string, wantCode int) orderResponse {
	t.Helper()

	return h.checkoutWithPayloadAndIdempotencyKeyExpectCode(t, shopperToken, map[string]any{"paymentMethod": "card"}, idempotencyKey, wantCode)
}

func (h e2eHarness) checkoutWithPayloadAndIdempotencyKeyExpectCode(t *testing.T, shopperToken string, payload map[string]any, idempotencyKey string, wantCode int) orderResponse {
	t.Helper()

	response := doJSON[orderResponse](t, h.httpClient, requestSpec{
		Method: http.MethodPost,
		URL:    h.gatewayBaseURL + "/v1/orders",
		Auth:   shopperToken,
		Headers: map[string]string{
			"Idempotency-Key": idempotencyKey,
		},
		Body:     payload,
		WantCode: wantCode,
	})

	require.NotEmpty(t, response.OrderID, "checkout response must include orderId")

	return response
}

func (h e2eHarness) checkoutErrorExpectCode(t *testing.T, shopperToken string, wantCode int) errorResponse {
	t.Helper()

	return h.checkoutWithIdempotencyKeyExpectError(t, shopperToken, h.newIdempotencyKey(), wantCode)
}

func (h e2eHarness) checkoutWithIdempotencyKeyExpectError(t *testing.T, shopperToken, idempotencyKey string, wantCode int) errorResponse {
	t.Helper()

	return h.checkoutWithPayloadAndIdempotencyKeyExpectError(t, shopperToken, map[string]any{"paymentMethod": "card"}, idempotencyKey, wantCode)
}

func (h e2eHarness) checkoutWithPayloadAndIdempotencyKeyExpectError(t *testing.T, shopperToken string, payload map[string]any, idempotencyKey string, wantCode int) errorResponse {
	t.Helper()

	return doJSON[errorResponse](t, h.httpClient, requestSpec{
		Method: http.MethodPost,
		URL:    h.gatewayBaseURL + "/v1/orders",
		Auth:   shopperToken,
		Headers: map[string]string{
			"Idempotency-Key": idempotencyKey,
		},
		Body:     payload,
		WantCode: wantCode,
	})
}

func (h e2eHarness) waitForOrderConfirmed(t *testing.T, shopperToken, orderID string) {
	t.Helper()

	h.waitForOrderStatus(t, shopperToken, orderID, "confirmed")
}

func (h e2eHarness) waitForOrderCancelled(t *testing.T, shopperToken, orderID string) {
	t.Helper()

	h.waitForOrderStatus(t, shopperToken, orderID, "cancelled")
}

func (h e2eHarness) waitForOrderStatus(t *testing.T, shopperToken, orderID, wantStatus string) {
	t.Helper()

	require.NotEmpty(t, orderID)

	deadline := time.Now().Add(h.orderWaitTimeout)
	var lastStatus string

	for time.Now().Before(deadline) {
		order := doJSON[orderResponse](t, h.httpClient, requestSpec{
			Method:   http.MethodGet,
			URL:      h.gatewayBaseURL + "/v1/orders/" + orderID,
			Auth:     shopperToken,
			WantCode: http.StatusOK,
		})

		lastStatus = order.Status
		if order.Status == wantStatus {
			return
		}

		time.Sleep(h.pollInterval)
	}

	require.Failf(t, "order did not reach expected status", "orderId=%s wantStatus=%q lastStatus=%q", orderID, wantStatus, lastStatus)
}

func (h e2eHarness) assertReadyEndpoint(t *testing.T, spec readinessSpec) {
	t.Helper()

	deadline := time.Now().Add(h.readyTimeout)
	var lastStatus int
	var lastBody string
	var lastErr error

	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, h.gatewayBaseURL+spec.path, nil)
		require.NoError(t, err)

		resp, err := h.httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(h.pollInterval)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, readErr)

		lastStatus = resp.StatusCode
		lastBody = string(body)
		lastErr = nil

		if slices.Contains(spec.wantStatuses, resp.StatusCode) {
			return
		}

		time.Sleep(h.pollInterval)
	}

	if lastErr != nil {
		require.Failf(
			t,
			"required service did not become ready",
			"%s readiness check timed out at %s%s after retries: last error=%v. Start infra/services per docs/architecture/local-environment-bootstrap.md",
			spec.name,
			h.gatewayBaseURL,
			spec.path,
			lastErr,
		)
		return
	}

	require.Failf(
		t,
		"required service did not become ready",
		"%s readiness check timed out at %s%s after retries: want status in %v last status=%d body=%q",
		spec.name,
		h.gatewayBaseURL,
		spec.path,
		spec.wantStatuses,
		lastStatus,
		lastBody,
	)
}

type requestSpec struct {
	Method   string
	URL      string
	Auth     string
	Headers  map[string]string
	Body     any
	WantCode int
}

type transient429RetryConfig struct {
	maxWait  time.Duration
	interval time.Duration
}

func transient429RetryConfigFromEnv(t *testing.T) transient429RetryConfig {
	t.Helper()

	maxWait := durationEnvOrDefault(t, envTransient429RetryMaxWait, defaultTransient429RetryMaxWait)
	interval := durationEnvOrDefault(t, envTransient429RetryInterval, defaultTransient429RetryInterval)

	require.Greaterf(t, maxWait, time.Duration(0), "%s must be greater than zero", envTransient429RetryMaxWait)
	require.Greaterf(t, interval, time.Duration(0), "%s must be greater than zero", envTransient429RetryInterval)

	return transient429RetryConfig{maxWait: maxWait, interval: interval}
}

func shouldRetryTransient429(gotStatus, wantStatus int) bool {
	return gotStatus == http.StatusTooManyRequests && wantStatus != http.StatusTooManyRequests
}

func doJSON[T any](t *testing.T, client *http.Client, spec requestSpec) T {
	t.Helper()

	config := transient429RetryConfigFromEnv(t)

	var requestBody []byte
	if spec.Body != nil {
		payload, err := json.Marshal(spec.Body)
		require.NoError(t, err)
		requestBody = payload
	}
	deadline := time.Now().Add(config.maxWait)
	attempt := 0

	var finalStatus int
	var finalBody []byte

	for {
		if !time.Now().Before(deadline) {
			break
		}

		attempt++

		var bodyReader io.Reader
		if requestBody != nil {
			bodyReader = bytes.NewReader(requestBody)
		}

		req, err := http.NewRequest(spec.Method, spec.URL, bodyReader)
		require.NoError(t, err)
		attemptCtx, cancel := context.WithDeadline(req.Context(), deadline)
		req = req.WithContext(attemptCtx)
		if spec.Body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if spec.Auth != "" {
			req.Header.Set("Authorization", "Bearer "+spec.Auth)
		}
		for key, value := range spec.Headers {
			req.Header.Set(key, value)
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
		}
		require.NoError(t, err)

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		require.NoError(t, readErr)

		finalStatus = resp.StatusCode
		finalBody = body

		if finalStatus == spec.WantCode {
			break
		}

		if !shouldRetryTransient429(finalStatus, spec.WantCode) {
			break
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		sleepDuration := config.interval
		if sleepDuration > remaining {
			sleepDuration = remaining
		}

		time.Sleep(sleepDuration)
	}

	require.Equalf(
		t,
		spec.WantCode,
		finalStatus,
		"%s %s expected %d got %d body=%q attempts=%d transient_429_retry(max_wait=%s interval=%s)",
		spec.Method,
		spec.URL,
		spec.WantCode,
		finalStatus,
		string(finalBody),
		attempt,
		config.maxWait,
		config.interval,
	)

	var decoded T
	if len(finalBody) == 0 {
		return decoded
	}

	require.NoErrorf(t, json.Unmarshal(finalBody, &decoded), "decode response for %s %s failed; body=%q", spec.Method, spec.URL, string(finalBody))

	return decoded
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	return value
}

func durationEnvOrDefault(t *testing.T, name string, fallback time.Duration) time.Duration {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}

	if parsed, err := time.ParseDuration(raw); err == nil {
		return parsed
	}

	seconds, err := strconv.Atoi(raw)
	require.NoErrorf(t, err, "%s must be valid duration (e.g. 45s, 1m) or integer seconds", name)
	require.Greaterf(t, seconds, 0, "%s must be greater than zero", name)

	return time.Duration(seconds) * time.Second
}

func validateBaseURL(t *testing.T, rawURL, envName string) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoErrorf(t, err, "%s must be valid URL", envName)
	require.NotEmptyf(t, parsed.Scheme, "%s must include scheme", envName)
	require.NotEmptyf(t, parsed.Host, "%s must include host", envName)
}

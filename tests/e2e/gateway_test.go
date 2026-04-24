package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGatewayRoutesNonAuthPathsToOwningServices(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessIdentity, readinessProduct, readinessCart, readinessOrder)

	gatewayBaseURL := harness.gatewayBaseURL
	shopperToken := harness.registerShopper(t)

	statusCode, responseBody, responseHeaders := doRequestWithHeaders(
		t,
		harness.httpClient,
		http.MethodGet,
		gatewayBaseURL+"/v1/products",
		nil,
		nil,
		http.StatusOK,
	)
	require.Equalf(t, http.StatusOK, statusCode, "GET %s expected %d got %d body=%q", gatewayBaseURL+"/v1/products", http.StatusOK, statusCode, responseBody)
	assertBaselineSecurityHeaders(t, responseHeaders)

	var productsEnvelope map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(responseBody), &productsEnvelope), "GET %s response must be valid JSON object", gatewayBaseURL+"/v1/products")
	_, hasItems := productsEnvelope["items"]
	require.Truef(t, hasItems, "GET %s response must include %q field; body=%q", gatewayBaseURL+"/v1/products", "items", responseBody)

	cartErr := assertRouteJSONErrorCode(
		t,
		harness.httpClient,
		http.MethodPost,
		gatewayBaseURL+"/v1/cart/items",
		map[string]any{"sku": harness.newUnknownSKU(), "quantity": 1},
		map[string]string{"Authorization": "Bearer " + shopperToken},
		http.StatusNotFound,
	)
	require.Equal(t, "product_not_found", cartErr.Code)

	orderErr := assertRouteJSONErrorCode(
		t,
		harness.httpClient,
		http.MethodPost,
		gatewayBaseURL+"/v1/orders",
		map[string]any{"paymentMethod": "card"},
		map[string]string{
			"Authorization":   "Bearer " + shopperToken,
			"Idempotency-Key": harness.newIdempotencyKey(),
		},
		http.StatusConflict,
	)
	require.Equal(t, "CART_EMPTY", orderErr.Code)
}

func TestGatewayReturnsExpected404And405ForUncoveredCombinations(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessProduct, readinessCart, readinessOrder)

	gatewayBaseURL := harness.gatewayBaseURL

	assertRouteStatus(t, harness.httpClient, http.MethodPatch, gatewayBaseURL+"/v1/products", nil, nil, http.StatusMethodNotAllowed)
	assertRouteStatus(t, harness.httpClient, http.MethodGet, gatewayBaseURL+"/v1/cart/items", nil, nil, http.StatusMethodNotAllowed)
	assertRouteStatus(t, harness.httpClient, http.MethodDelete, gatewayBaseURL+"/v1/orders", nil, nil, http.StatusMethodNotAllowed)

	nonExistentProductID := uuid.NewString()
	assertRouteStatus(t, harness.httpClient, http.MethodGet, gatewayBaseURL+"/v1/products/"+nonExistentProductID, nil, nil, http.StatusNotFound)
}

func TestGatewayProfileRouteAuthForwarding(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessIdentity)

	gatewayBaseURL := harness.gatewayBaseURL

	registerResponse := doJSON[authTokensResponse](t, harness.httpClient, requestSpec{
		Method: http.MethodPost,
		URL:    gatewayBaseURL + "/v1/auth/register",
		Body: map[string]any{
			"email":       fmt.Sprintf("gateway-shopper-%s@example.com", uuid.NewString()),
			"password":    envOrDefault("E2E_SHOPPER_PASSWORD", defaultShopperPassword),
			"displayName": "Gateway Shopper",
		},
		WantCode: http.StatusCreated,
	})

	require.NotEmpty(t, registerResponse.AccessToken, "shopper accessToken must be returned")

	assertRouteStatus(
		t,
		harness.httpClient,
		http.MethodGet,
		gatewayBaseURL+"/v1/profile/me",
		nil,
		map[string]string{"Authorization": "Bearer " + registerResponse.AccessToken},
		http.StatusOK,
	)

	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{
			name:    "missing token",
			headers: nil,
		},
		{
			name: "malformed token",
			headers: map[string]string{
				"Authorization": "Bearer malformed-token",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertRouteStatus(t, harness.httpClient, http.MethodGet, gatewayBaseURL+"/v1/profile/me", nil, tc.headers, http.StatusUnauthorized)
		})
	}
}

func TestGatewayCorrelationIDPropagationInErrorEnvelope(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessIdentity)

	gatewayBaseURL := harness.gatewayBaseURL

	const correlationErrorPath = "/v1/auth/register-admin"

	for _, tc := range []struct {
		name                   string
		headers                map[string]string
		wantResponseRequestID  string
		wantGeneratedRequestID bool
		correlationAssertNote  string
	}{
		{
			name: "x-request-id preserved and mirrored in envelope",
			headers: map[string]string{
				"X-Request-Id": "e2e-request-id",
			},
			wantResponseRequestID: "e2e-request-id",
		},
		{
			name:                   "gateway generated request id when headers absent",
			headers:                nil,
			wantGeneratedRequestID: true,
		},
		{
			name: "x-correlation-id only follows observable request-id contract",
			headers: map[string]string{
				"X-Correlation-Id": "e2e-correlation-id-only",
			},
			correlationAssertNote: "observable contract: error envelope correlationId tracks response X-Request-Id",
		},
		{
			name: "both headers present follows request-id observable contract",
			headers: map[string]string{
				"X-Correlation-Id": "e2e-correlation-id-ignored-by-envelope",
				"X-Request-Id":     "e2e-request-id-priority",
			},
			wantResponseRequestID: "e2e-request-id-priority",
			correlationAssertNote: "observable contract: request-id context drives envelope correlationId",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statusCode, responseBody, responseHeaders := doRequestWithHeaders(t, harness.httpClient, http.MethodPost, gatewayBaseURL+correlationErrorPath, nil, tc.headers, http.StatusUnauthorized)
			require.Equalf(t, http.StatusUnauthorized, statusCode, "POST %s expected %d got %d body=%q", correlationErrorPath, http.StatusUnauthorized, statusCode, responseBody)

			var response struct {
				CorrelationID string `json:"correlationId"`
			}
			require.NoErrorf(t, json.Unmarshal([]byte(responseBody), &response), "response must be valid JSON error envelope; body=%q", responseBody)

			responseRequestID := responseHeaders.Get("X-Request-Id")
			require.NotEmpty(t, responseRequestID, "gateway must return non-empty X-Request-Id header for observable request tracing")

			if tc.wantResponseRequestID != "" {
				require.Equalf(t, tc.wantResponseRequestID, responseRequestID, "gateway must preserve caller X-Request-Id in response header; note=%s", tc.correlationAssertNote)
			}

			require.Equalf(t, responseRequestID, response.CorrelationID, "error envelope correlationId must equal response X-Request-Id (observable contract); note=%s", tc.correlationAssertNote)
		})
	}
}

func TestGatewayHealthzAndFallbackRoutesDoNotRequireBaselineSecurityHeaders(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway)

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "healthz", path: "/healthz"},
		{name: "fallback root", path: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statusCode, responseBody, responseHeaders := doRequestOnceWithHeaders(
				t,
				harness.httpClient,
				http.MethodGet,
				harness.gatewayBaseURL+tc.path,
				nil,
				nil,
			)

			require.Equalf(t, http.StatusOK, statusCode, "GET %s expected %d got %d body=%q", tc.path, http.StatusOK, statusCode, responseBody)
			assertBaselineSecurityHeadersAbsent(t, responseHeaders)
		})
	}
}

func doRequestOnceWithHeaders(
	t *testing.T,
	client *http.Client,
	method, requestURL string,
	body any,
	headers map[string]string,
) (int, string, http.Header) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, requestURL, bodyReader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, strings.TrimSpace(string(responseBody)), resp.Header.Clone()
}

func TestGatewayRateLimitingOnProductsRouteReturnsExpected429Contract(t *testing.T) {
	harness := newE2EHarness(t)
	harness.assertServicesReady(t, readinessGateway, readinessProduct)

	gatewayBaseURL := harness.gatewayBaseURL
	rateLimitClient := &http.Client{
		Timeout: harness.httpClient.Timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        2048,
			MaxIdleConnsPerHost: 2048,
			DisableCompression:  true,
			DisableKeepAlives:   true,
			ForceAttemptHTTP2:   false,
		},
	}
	t.Cleanup(rateLimitClient.CloseIdleConnections)

	const (
		maxBursts               = 8
		requestsPerBurst        = 300
		requiredRateLimitStatus = http.StatusTooManyRequests
	)

	type requestResult struct {
		statusCode int
		body       string
		headers    http.Header
		err        error
	}

	var first429 *requestResult
	rateLimitedResponses := 0
	transportErrors := 0

	runBurst := func(requestsCount int) []requestResult {
		results := make(chan requestResult, requestsCount)
		start := make(chan struct{})

		var wg sync.WaitGroup
		for range requestsCount {
			wg.Go(func() {
				<-start

				req, err := http.NewRequest(http.MethodGet, gatewayBaseURL+"/v1/products", nil)
				if err != nil {
					results <- requestResult{err: fmt.Errorf("build request: %w", err)}
					return
				}

				resp, err := rateLimitClient.Do(req)
				if err != nil {
					results <- requestResult{err: fmt.Errorf("execute request: %w", err)}
					return
				}
				defer resp.Body.Close()

				responseBody, err := io.ReadAll(resp.Body)
				if err != nil {
					results <- requestResult{err: fmt.Errorf("read response body: %w", err)}
					return
				}

				results <- requestResult{
					statusCode: resp.StatusCode,
					body:       strings.TrimSpace(string(responseBody)),
					headers:    resp.Header.Clone(),
				}
			})
		}

		close(start)
		wg.Wait()
		close(results)

		collected := make([]requestResult, 0, requestsCount)
		for result := range results {
			collected = append(collected, result)
		}

		return collected
	}

	for burst := 0; burst < maxBursts && first429 == nil; burst++ {
		for _, result := range runBurst(requestsPerBurst) {
			if result.err != nil {
				transportErrors++
				continue
			}

			if result.statusCode == requiredRateLimitStatus {
				rateLimitedResponses++
				if first429 == nil {
					captured := result
					first429 = &captured
				}
			}
		}
	}

	require.GreaterOrEqual(t, rateLimitedResponses, 1,
		"rate-limited burst must produce at least one HTTP 429 response (transport_errors=%d)",
		transportErrors)
	require.NotNil(t, first429, "expected to capture at least one HTTP 429 response")

	require.Equal(t, http.StatusTooManyRequests, first429.statusCode)
	require.Contains(t, strings.ToLower(first429.headers.Get("Content-Type")), "application/json")
	require.Equal(t, "1", first429.headers.Get("Retry-After"))
	assertBaselineSecurityHeaders(t, first429.headers)

	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	require.NoErrorf(t, json.Unmarshal([]byte(first429.body), &response), "429 response must be valid JSON; body=%q", first429.body)
	require.Equalf(t, "RATE_LIMITED", response.Error.Code, "429 response must expose RATE_LIMITED code; body=%q", first429.body)
	require.NotEmptyf(t, response.Error.Message, "429 response message must not be empty; body=%q", first429.body)
}

func assertBaselineSecurityHeaders(t *testing.T, headers http.Header) {
	t.Helper()

	require.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	require.Equal(t, "DENY", headers.Get("X-Frame-Options"))
	require.Equal(t, "no-referrer", headers.Get("Referrer-Policy"))
	require.Equal(t, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'", headers.Get("Content-Security-Policy"))
}

func assertBaselineSecurityHeadersAbsent(t *testing.T, headers http.Header) {
	t.Helper()

	require.Empty(t, headers.Get("X-Content-Type-Options"))
	require.Empty(t, headers.Get("X-Frame-Options"))
	require.Empty(t, headers.Get("Referrer-Policy"))
	require.Empty(t, headers.Get("Content-Security-Policy"))
}

func assertRouteStatus(
	t *testing.T,
	client *http.Client,
	method, requestURL string,
	body any,
	headers map[string]string,
	wantStatus int) {
	t.Helper()

	statusCode, responseBody := doRequest(t, client, method, requestURL, body, headers, wantStatus)
	require.Equalf(t, wantStatus, statusCode, "%s %s expected %d got %d body=%q", method, requestURL, wantStatus, statusCode, responseBody)
}

func assertRouteStatusAndJSONShape(
	t *testing.T,
	client *http.Client,
	method, requestURL string,
	body any,
	headers map[string]string,
	wantStatus int,
	requiredFields []string) {
	t.Helper()

	statusCode, responseBody := doRequest(t, client, method, requestURL, body, headers, wantStatus)
	require.Equalf(t, wantStatus, statusCode, "%s %s expected %d got %d body=%q", method, requestURL, wantStatus, statusCode, responseBody)

	var envelope map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(responseBody), &envelope), "%s %s response must be valid JSON object", method, requestURL)

	for _, key := range requiredFields {
		_, ok := envelope[key]
		require.Truef(t, ok, "%s %s response must include %q field; body=%q", method, requestURL, key, responseBody)
	}
}

func assertRouteJSONErrorCode(
	t *testing.T,
	client *http.Client,
	method, requestURL string,
	body any,
	headers map[string]string,
	wantStatus int) errorResponse {
	t.Helper()

	statusCode, responseBody := doRequest(t, client, method, requestURL, body, headers, wantStatus)
	require.Equalf(t, wantStatus, statusCode, "%s %s expected %d got %d body=%q", method, requestURL, wantStatus, statusCode, responseBody)

	var response errorResponse
	require.NoErrorf(t, json.Unmarshal([]byte(responseBody), &response), "%s %s response must be valid JSON error envelope", method, requestURL)
	require.NotEmptyf(t, response.Code, "%s %s error response code must not be empty; body=%q", method, requestURL, responseBody)
	require.NotEmptyf(t, response.Message, "%s %s error response message must not be empty; body=%q", method, requestURL, responseBody)

	return response
}

func doRequest(
	t *testing.T,
	client *http.Client,
	method, requestURL string,
	body any,
	headers map[string]string,
	wantStatus int) (int, string) {
	t.Helper()

	statusCode, responseBody, _ := doRequestWithHeaders(t, client, method, requestURL, body, headers, wantStatus)

	return statusCode, responseBody
}

func doRequestWithHeaders(
	t *testing.T,
	client *http.Client,
	method, requestURL string,
	body any,
	headers map[string]string,
	wantStatus int) (int, string, http.Header) {
	t.Helper()

	config := transient429RetryConfigFromEnv(t)

	var requestBody []byte
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		requestBody = payload
	}
	deadline := time.Now().Add(config.maxWait)

	var finalStatus int
	var finalBody []byte
	var finalHeaders http.Header

	for {
		if !time.Now().Before(deadline) {
			break
		}

		var bodyReader io.Reader
		if requestBody != nil {
			bodyReader = bytes.NewReader(requestBody)
		}

		req, err := http.NewRequest(method, requestURL, bodyReader)
		require.NoError(t, err)
		attemptCtx, cancel := context.WithDeadline(req.Context(), deadline)
		req = req.WithContext(attemptCtx)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
		}
		require.NoError(t, err)

		responseBodyBytes, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		require.NoError(t, readErr)

		finalStatus = resp.StatusCode
		finalBody = responseBodyBytes
		finalHeaders = resp.Header.Clone()

		if !shouldRetryTransient429(finalStatus, wantStatus) {
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

	return finalStatus, strings.TrimSpace(string(finalBody)), finalHeaders
}

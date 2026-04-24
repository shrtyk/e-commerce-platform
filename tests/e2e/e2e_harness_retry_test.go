package e2e

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShouldRetryTransient429(t *testing.T) {
	t.Run("retry when got 429 but expected non-429", func(t *testing.T) {
		require.True(t, shouldRetryTransient429(http.StatusTooManyRequests, http.StatusUnauthorized))
	})

	t.Run("do not retry when expected 429", func(t *testing.T) {
		require.False(t, shouldRetryTransient429(http.StatusTooManyRequests, http.StatusTooManyRequests))
	})

	t.Run("do not retry when got non-429", func(t *testing.T) {
		require.False(t, shouldRetryTransient429(http.StatusUnauthorized, http.StatusUnauthorized))
	})
}

func TestTransient429RetryConfigFromEnvUsesDefaults(t *testing.T) {
	t.Setenv(envTransient429RetryMaxWait, "")
	t.Setenv(envTransient429RetryInterval, "")

	config := transient429RetryConfigFromEnv(t)

	require.Equal(t, defaultTransient429RetryMaxWait, config.maxWait)
	require.Equal(t, defaultTransient429RetryInterval, config.interval)
}

func TestTransient429RetryConfigFromEnvUsesOverrideValues(t *testing.T) {
	t.Setenv(envTransient429RetryMaxWait, "3s")
	t.Setenv(envTransient429RetryInterval, "150ms")

	config := transient429RetryConfigFromEnv(t)

	require.Equal(t, 3*time.Second, config.maxWait)
	require.Equal(t, 150*time.Millisecond, config.interval)
}

func TestTransient429RetryConfigFromEnvRejectsInvalidValues(t *testing.T) {
	t.Run("invalid max wait format fails", func(t *testing.T) {
		err := runRetryConfigProbe(t, "not-a-duration", "100ms")
		require.Error(t, err)
	})

	t.Run("zero interval fails", func(t *testing.T) {
		err := runRetryConfigProbe(t, "2s", "0s")
		require.Error(t, err)
	})
}

func TestDoRequestWithHeadersTransient429RetryDoesNotAttemptAfterDeadline(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"RATE_LIMITED","message":"rate limited"}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv(envTransient429RetryMaxWait, "80ms")
	t.Setenv(envTransient429RetryInterval, "200ms")

	start := time.Now()
	statusCode, _, _ := doRequestWithHeaders(t, server.Client(), http.MethodGet, server.URL, nil, nil, http.StatusOK)
	elapsed := time.Since(start)

	require.Equal(t, http.StatusTooManyRequests, statusCode)
	require.Equal(t, int32(1), attempts.Load(), "retry loop must not start post-deadline extra attempt")
	require.Less(t, elapsed, 350*time.Millisecond)
}

func TestDoRequestWithHeadersTransient429RetryBindsAttemptContextDeadline(t *testing.T) {
	t.Setenv(envTransient429RetryMaxWait, "220ms")
	t.Setenv(envTransient429RetryInterval, "40ms")

	var attempts atomic.Int32
	hasDL := make([]bool, 0, 2)
	deadlines := make([]time.Time, 0, 2)

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			dl, ok := req.Context().Deadline()
			hasDL = append(hasDL, ok)
			deadlines = append(deadlines, dl)

			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"code":"RATE_LIMITED","message":"retry"}`)),
			}, nil
		}),
	}

	start := time.Now()
	statusCode, _, _ := doRequestWithHeaders(t, client, http.MethodGet, "http://example.test/limited", nil, nil, http.StatusOK)
	elapsed := time.Since(start)

	require.Equal(t, http.StatusTooManyRequests, statusCode)
	require.GreaterOrEqual(t, attempts.Load(), int32(2), "retry loop must issue second attempt before deadline")
	require.Lessf(t, elapsed, 700*time.Millisecond, "retry flow must stay deadline-bounded; elapsed=%s", elapsed)

	require.GreaterOrEqual(t, len(hasDL), 2)
	for i := range hasDL {
		require.True(t, hasDL[i], "retry request context must be deadline-bound")
	}
	require.GreaterOrEqual(t, len(deadlines), 2)
	for i := 1; i < len(deadlines); i++ {
		require.Equal(t, deadlines[0], deadlines[i], "all retry attempts must share same max-wait deadline")
	}
	require.WithinDuration(t, start.Add(220*time.Millisecond), deadlines[0], 80*time.Millisecond)
}

func TestDoRequestWithHeadersExpected429DoesNotRetry(t *testing.T) {
	t.Setenv(envTransient429RetryMaxWait, "250ms")
	t.Setenv(envTransient429RetryInterval, "10ms")

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"RATE_LIMITED","message":"expected"}`))
	}))
	t.Cleanup(server.Close)

	statusCode, _, _ := doRequestWithHeaders(t, server.Client(), http.MethodGet, server.URL, nil, nil, http.StatusTooManyRequests)

	require.Equal(t, http.StatusTooManyRequests, statusCode)
	require.Equal(t, int32(1), attempts.Load(), "expected 429 must bypass transient retry")
}

func TestTransient429RetryConfigProbe(t *testing.T) {
	if os.Getenv("GO_WANT_TRANSIENT_429_RETRY_CONFIG_PROBE") != "1" {
		t.Skip("probe-only test")
	}

	_ = transient429RetryConfigFromEnv(t)
}

func runRetryConfigProbe(t *testing.T, maxWait, interval string) error {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestTransient429RetryConfigProbe$", "-test.v")
	cmd.Env = append(
		os.Environ(),
		"GO_WANT_TRANSIENT_429_RETRY_CONFIG_PROBE=1",
		envTransient429RetryMaxWait+"="+maxWait,
		envTransient429RetryInterval+"="+interval,
	)

	return cmd.Run()
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

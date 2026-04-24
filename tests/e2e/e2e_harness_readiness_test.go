package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadinessServiceSpecIdentityAcceptsAuthStatusSet(t *testing.T) {
	h := e2eHarness{}

	spec := h.readinessServiceSpec(t, readinessIdentity)

	require.Equal(t, "identity-svc", spec.name)
	require.Equal(t, "/v1/profile/me", spec.path)
	require.ElementsMatch(t, []int{http.StatusUnauthorized, http.StatusForbidden}, spec.wantStatuses)
}

func TestReadinessServiceSpecOrderAcceptsMethodMatrixAndAuthStatusSet(t *testing.T) {
	h := e2eHarness{}

	spec := h.readinessServiceSpec(t, readinessOrder)

	require.Equal(t, "order-svc", spec.name)
	require.Equal(t, "/v1/orders", spec.path)
	require.ElementsMatch(t, []int{http.StatusMethodNotAllowed, http.StatusUnauthorized, http.StatusForbidden}, spec.wantStatuses)
}

func TestSupportedReadinessServicesSet(t *testing.T) {
	require.Equal(t,
		[]readinessService{
			readinessGateway,
			readinessIdentity,
			readinessProduct,
			readinessCart,
			readinessOrder,
		},
		supportedReadinessServices,
	)
}

func TestSupportedReadinessServicesHaveSpecs(t *testing.T) {
	h := e2eHarness{}

	for _, service := range supportedReadinessServices {
		spec := h.readinessServiceSpec(t, service)
		require.NotEmpty(t, spec.name)
		require.NotEmpty(t, spec.path)
		require.NotEmpty(t, spec.wantStatuses)
	}
}

package jwt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTokenVerifierPanicsWhenSecretOrIssuerInvalid(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		issuer  string
		wantErr string
	}{
		{name: "empty key", key: "", issuer: "identity-svc", wantErr: "field \"TokenVerifier.Key\" must be non-empty"},
		{name: "whitespace key", key: "   ", issuer: "identity-svc", wantErr: "field \"TokenVerifier.Key\" must be non-empty"},
		{name: "empty issuer", key: "secret-key", issuer: "", wantErr: "field \"TokenVerifier.Issuer\" must be non-empty"},
		{name: "whitespace issuer", key: "secret-key", issuer: "   ", wantErr: "field \"TokenVerifier.Issuer\" must be non-empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.PanicsWithError(t, tt.wantErr, func() {
				_ = NewTokenVerifier(tt.key, tt.issuer)
			})
		})
	}
}

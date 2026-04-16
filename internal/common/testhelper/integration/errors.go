package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func EnsureNoError(t *testing.T, err error, action string) {
	if err == nil {
		return
	}

	if t != nil {
		t.Helper()
		require.NoError(t, err, action)
		return
	}

	panic(fmt.Errorf("%s: %w", action, err))
}

//go:build integration
// +build integration

package integration

import (
	"os"
	"testing"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/testhelper"
)

func TestMain(m *testing.M) {
	testhelper.StartSharedHarness(nil)

	code := m.Run()
	testhelper.StopSharedHarness()

	os.Exit(code)
}

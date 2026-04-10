//go:build integration
// +build integration

package integration

import (
	"os"
	"testing"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/testhelper"
)

func TestMain(m *testing.M) {
	db := testhelper.StartSharedTestDB(nil)
	testhelper.TestDB = db

	code := m.Run()
	testhelper.StopSharedTestDB()

	os.Exit(code)
}

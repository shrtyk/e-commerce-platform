//go:build integration
// +build integration

package integration

import (
	"os"
	"testing"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/testhelper"
)

func TestMain(m *testing.M) {
	db := testhelper.StartSharedTestDB(nil)
	redisClient := testhelper.StartSharedTestRedis(nil)

	testhelper.TestDB = db
	testhelper.TestRedis = redisClient

	code := m.Run()

	testhelper.StopSharedTestRedis()
	testhelper.StopSharedTestDB()

	os.Exit(code)
}

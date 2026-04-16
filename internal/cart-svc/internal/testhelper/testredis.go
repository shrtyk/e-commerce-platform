package testhelper

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	redislib "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var TestRedis *redislib.Client

var sharedRedisContainer testcontainers.Container
var sharedRedisOnce sync.Once
var sharedRedisInitErr error
var sharedRedisMu sync.Mutex

func StartSharedTestRedis(t *testing.T) *redislib.Client {
	if t != nil {
		t.Helper()
	}

	sharedRedisMu.Lock()
	if TestRedis != nil {
		client := TestRedis
		sharedRedisMu.Unlock()
		return client
	}
	sharedRedisMu.Unlock()

	var initErr error
	sharedRedisOnce.Do(func() {
		initErr = initSharedTestRedis()

		sharedRedisMu.Lock()
		sharedRedisInitErr = initErr
		sharedRedisMu.Unlock()
	})

	sharedRedisMu.Lock()
	if sharedRedisInitErr != nil {
		initErr = sharedRedisInitErr
	}
	sharedRedisMu.Unlock()

	if initErr != nil {
		sharedRedisMu.Lock()
		sharedRedisOnce = sync.Once{}
		sharedRedisMu.Unlock()

		ensureNoError(t, initErr, "start shared test redis")
	}

	sharedRedisMu.Lock()
	client := TestRedis
	sharedRedisMu.Unlock()
	if client == nil {
		ensureNoError(t, fmt.Errorf("shared test redis is nil"), "resolve shared test redis")
	}

	return client
}

func initSharedTestRedis() (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:8-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("start redis container: %w", err)
	}

	defer func() {
		if err == nil {
			return
		}

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = container.Terminate(cleanupCtx)
	}()

	host, err := container.Host(ctx)
	if err != nil {
		return fmt.Errorf("redis host: %w", err)
	}

	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		return fmt.Errorf("redis mapped port: %w", err)
	}

	redisAddr := net.JoinHostPort(host, port.Port())

	client := redislib.NewClient(&redislib.Options{Addr: redisAddr})

	defer func() {
		if err == nil {
			return
		}

		_ = client.Close()
	}()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer pingCancel()
	for {
		if pingErr := client.Ping(pingCtx).Err(); pingErr == nil {
			break
		}

		select {
		case <-pingCtx.Done():
			return fmt.Errorf("ping redis: %w", pingCtx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}

	sharedRedisMu.Lock()
	TestRedis = client
	sharedRedisContainer = container
	sharedRedisMu.Unlock()

	return nil
}

func StopSharedTestRedis() {
	sharedRedisMu.Lock()
	client := TestRedis
	container := sharedRedisContainer
	TestRedis = nil
	sharedRedisContainer = nil
	sharedRedisInitErr = nil
	sharedRedisOnce = sync.Once{}
	sharedRedisMu.Unlock()

	if client != nil {
		if err := client.Close(); err != nil {
			panic(fmt.Errorf("close shared test redis: %w", err))
		}
	}

	if container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := container.Terminate(ctx); err != nil {
			panic(fmt.Errorf("terminate shared test redis container: %w", err))
		}
	}
}

func CleanupRedis(t *testing.T, client *redislib.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.FlushDB(ctx).Err()
	if err != nil {
		err = fmt.Errorf("flush redis within timeout: %w", err)
	}
	ensureNoError(t, err, "flush redis")
}

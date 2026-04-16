package integration

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

type RedisOptions struct {
	Image string
}

type SharedRedis struct {
	mu        sync.Mutex
	once      sync.Once
	initErr   error
	container testcontainers.Container
	client    *redislib.Client
}

func (s *SharedRedis) Start(t *testing.T, opts RedisOptions) *redislib.Client {
	if t != nil {
		t.Helper()
	}

	s.mu.Lock()
	if s.client != nil {
		client := s.client
		s.mu.Unlock()
		return client
	}
	s.mu.Unlock()

	var initErr error
	s.once.Do(func() {
		initErr = s.initRedis(opts)

		s.mu.Lock()
		s.initErr = initErr
		s.mu.Unlock()
	})

	s.mu.Lock()
	if s.initErr != nil {
		initErr = s.initErr
	}
	s.mu.Unlock()

	if initErr != nil {
		s.mu.Lock()
		s.once = sync.Once{}
		s.mu.Unlock()

		EnsureNoError(t, initErr, "start shared redis")
	}

	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		EnsureNoError(t, fmt.Errorf("shared redis is nil"), "resolve shared redis")
	}

	return client
}

func (s *SharedRedis) Stop() {
	s.mu.Lock()
	client := s.client
	container := s.container
	s.client = nil
	s.container = nil
	s.initErr = nil
	s.once = sync.Once{}
	s.mu.Unlock()

	if client != nil {
		if err := client.Close(); err != nil {
			panic(fmt.Errorf("close shared redis: %w", err))
		}
	}

	if container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := container.Terminate(ctx); err != nil {
			panic(fmt.Errorf("terminate shared redis container: %w", err))
		}
	}
}

func (s *SharedRedis) initRedis(opts RedisOptions) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        opts.Image,
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
		return fmt.Errorf("resolve redis host: %w", err)
	}

	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		return fmt.Errorf("resolve redis mapped port: %w", err)
	}

	client := redislib.NewClient(&redislib.Options{Addr: net.JoinHostPort(host, port.Port())})

	defer func() {
		if err == nil {
			return
		}

		_ = client.Close()
	}()

	if err := waitForRedis(ctx, client); err != nil {
		return fmt.Errorf("wait for redis readiness: %w", err)
	}

	s.mu.Lock()
	s.client = client
	s.container = container
	s.mu.Unlock()

	return nil
}

func waitForRedis(ctx context.Context, client *redislib.Client) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := client.Ping(ctx).Err(); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

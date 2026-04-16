package integration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufconnBufferSize = 1024 * 1024

func StartBufconnGRPCServer(t *testing.T, target string, server *grpc.Server) (*grpc.ClientConn, func()) {
	t.Helper()

	listener := bufconn.Listen(bufconnBufferSize)
	ready := make(chan struct{})
	serveDone := make(chan struct{})
	serveErr := make(chan error, 1)
	readyListener := &readySignalListener{Listener: listener, ready: ready}

	go func() {
		defer close(serveDone)
		if err := server.Serve(readyListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			select {
			case serveErr <- err:
			default:
			}
		}
	}()

	select {
	case <-ready:
	case err := <-serveErr:
		server.Stop()
		EnsureNoError(t, listener.Close(), "close bufconn listener after startup failure")
		<-serveDone
		t.Fatalf("grpc server failed before accepting connections: %v", err)
	case <-time.After(5 * time.Second):
		server.Stop()
		EnsureNoError(t, listener.Close(), "close bufconn listener after startup timeout")
		<-serveDone
		t.Fatal("grpc server did not start accepting connections")
	}

	dialer := func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}

	conn, err := grpc.NewClient(
		"passthrough:///"+target,
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		server.Stop()
		EnsureNoError(t, listener.Close(), "close bufconn listener after dial failure")
		<-serveDone
		t.Fatalf("create grpc client connection: %v", err)
	}

	stop := func() {
		EnsureNoError(t, conn.Close(), "close grpc client connection")
		server.Stop()
		EnsureNoError(t, listener.Close(), "close bufconn listener")
		<-serveDone
	}

	return conn, stop
}

type readySignalListener struct {
	net.Listener
	once  sync.Once
	ready chan struct{}
}

func (l *readySignalListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		close(l.ready)
	})

	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("accept connection: %w", err)
	}

	return conn, nil
}

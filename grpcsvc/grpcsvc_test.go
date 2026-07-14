package grpcsvc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dvislobokov/shost/grpcsvc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func newHealthServer() *grpc.Server {
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, health.NewServer())
	return srv
}

// start runs the service and returns the Start result channel after
// readiness.
func start(t *testing.T, svc *grpcsvc.Service, ctx context.Context) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- svc.Start(ctx) }()
	select {
	case <-svc.Ready():
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("service not ready in time")
	}
	return done
}

func dial(t *testing.T, addr string) healthpb.HealthClient {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return healthpb.NewHealthClient(conn)
}

func TestServeAndGracefulStop(t *testing.T) {
	svc := grpcsvc.New(":0", newHealthServer(), grpcsvc.WithName("api"))
	if svc.Name() != "api" {
		t.Fatalf("wrong name: %s", svc.Name())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := start(t, svc, ctx)

	if svc.Addr() == "" {
		t.Fatal("Addr is empty after ready")
	}
	client := dial(t, svc.Addr())
	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()
	resp, err := client.Check(rpcCtx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("unexpected status: %v", resp.Status)
	}

	// Host shutdown sequence: cancel the Start ctx, then call Stop.
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := svc.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestForcefulStopOnDeadline(t *testing.T) {
	srv := newHealthServer()
	svc := grpcsvc.New(":0", srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := start(t, svc, ctx)

	// Hold a Watch stream open so GracefulStop cannot finish.
	client := dial(t, svc.Addr())
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	stream, err := client.Watch(streamCtx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopCancel()
	err = svc.Stop(stopCtx)
	if err == nil || !strings.Contains(err.Error(), "closed forcefully") {
		t.Fatalf("expected forceful-close error, got: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after forceful stop")
	}
}

func TestListenErrorFailsStart(t *testing.T) {
	first := grpcsvc.New(":0", newHealthServer())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := start(t, first, ctx)

	second := grpcsvc.New(first.Addr(), newHealthServer())
	if err := second.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("expected listen error, got: %v", err)
	}

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = first.Stop(stopCtx)
	<-done
}

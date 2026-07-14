package grpcgw_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dvislobokov/shost/grpcgw"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

// pingRegister stands in for a protoc-generated RegisterXxxHandler: real
// transcoding needs generated stubs, so tests exercise the service
// lifecycle through a hand-registered path.
func pingRegister(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
	return mux.HandlePath("GET", "/ping", func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		w.Write([]byte("pong"))
	})
}

func start(t *testing.T, svc *grpcgw.Service, ctx context.Context) chan error {
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

func TestServeAndGracefulStop(t *testing.T) {
	svc := grpcgw.New(":0", "localhost:9090",
		grpcgw.Register(pingRegister),
		grpcgw.WithName("gateway"),
	)
	if svc.Name() != "gateway" {
		t.Fatalf("wrong name: %s", svc.Name())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := start(t, svc, ctx)

	resp, err := http.Get("http://" + svc.Addr() + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "pong" {
		t.Fatalf("unexpected response: %d %q", resp.StatusCode, body)
	}

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

func TestWithHandlerWrapsMux(t *testing.T) {
	svc := grpcgw.New(":0", "localhost:9090",
		grpcgw.Register(pingRegister),
		grpcgw.WithHandler(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Middleware", "on")
				next.ServeHTTP(w, r)
			})
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := start(t, svc, ctx)

	resp, err := http.Get("http://" + svc.Addr() + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("X-Middleware") != "on" {
		t.Fatal("middleware header missing")
	}

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = svc.Stop(stopCtx)
	<-done
}

func TestRegisterErrorFailsStart(t *testing.T) {
	boom := errors.New("register boom")
	svc := grpcgw.New(":0", "localhost:9090",
		grpcgw.Register(func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
			return boom
		}),
	)
	if err := svc.Start(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("expected register error, got: %v", err)
	}
}

func TestStopBeforeStartIsNoop(t *testing.T) {
	svc := grpcgw.New(":0", "localhost:9090", grpcgw.Register(pingRegister))
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start should be a no-op, got: %v", err)
	}
}

func TestNewWithoutRegisterPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "Register") {
			t.Fatalf("expected panic about missing Register, got: %v", r)
		}
	}()
	grpcgw.New(":0", "localhost:9090")
}

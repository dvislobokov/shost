package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dvislobokov/shost/health"
)

type body struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func do(t *testing.T, h http.Handler) (int, body) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var b body
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("invalid JSON response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, b
}

func TestLiveHandler(t *testing.T) {
	reg := health.NewRegistry(
		health.CheckerFunc("db", func(ctx context.Context) error { return nil }),
	)
	code, b := do(t, reg.LiveHandler())
	if code != http.StatusOK || b.Status != "ok" || b.Checks["db"] != "ok" {
		t.Fatalf("expected healthy, got %d %+v", code, b)
	}

	reg.Add(health.CheckerFunc("cache", func(ctx context.Context) error {
		return errors.New("connection refused")
	}))
	code, b = do(t, reg.LiveHandler())
	if code != http.StatusServiceUnavailable || b.Status != "unhealthy" {
		t.Fatalf("expected unhealthy, got %d %+v", code, b)
	}
	if b.Checks["cache"] != "connection refused" || b.Checks["db"] != "ok" {
		t.Fatalf("unexpected checks: %+v", b.Checks)
	}
}

func TestReadyHandlerFollowsFlag(t *testing.T) {
	reg := health.NewRegistry()

	code, _ := do(t, reg.ReadyHandler())
	if code != http.StatusServiceUnavailable {
		t.Fatalf("expected not ready before SetReady, got %d", code)
	}

	reg.SetReady(true)
	code, b := do(t, reg.ReadyHandler())
	if code != http.StatusOK || b.Status != "ok" {
		t.Fatalf("expected ready, got %d %+v", code, b)
	}

	reg.SetReady(false)
	if code, _ := do(t, reg.ReadyHandler()); code != http.StatusServiceUnavailable {
		t.Fatalf("expected not ready after SetReady(false), got %d", code)
	}
}

func TestPanicInCheckerIsUnhealthy(t *testing.T) {
	reg := health.NewRegistry(
		health.CheckerFunc("bad", func(ctx context.Context) error { panic("check boom") }),
	)
	code, b := do(t, reg.LiveHandler())
	if code != http.StatusServiceUnavailable || b.Checks["bad"] != "panic: check boom" {
		t.Fatalf("expected panic reported as unhealthy, got %d %+v", code, b)
	}
}

func TestMountRoutes(t *testing.T) {
	reg := health.NewRegistry()
	reg.SetReady(true)
	mux := http.NewServeMux()
	reg.Mount(mux)

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
	}
}

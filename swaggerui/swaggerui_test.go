package swaggerui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dvislobokov/shost/swaggerui"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestIndex(t *testing.T) {
	h := swaggerui.Handler(swaggerui.WithTitle("Billing API"))
	for _, path := range []string{"/", "/index.html"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: content type %q", path, ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<title>Billing API</title>") {
			t.Fatalf("%s: title missing in body", path)
		}
		if !strings.Contains(body, `"./swagger-ui-bundle.js"`) {
			t.Fatalf("%s: relative bundle reference missing", path)
		}
	}
}

func TestBundledAssets(t *testing.T) {
	h := swaggerui.Handler()
	for path, ct := range map[string]string{
		"/swagger-ui.css":       "text/css",
		"/swagger-ui-bundle.js": "text/javascript",
		"/oauth2-redirect.html": "text/html",
		"/oauth2-redirect.js":   "text/javascript",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, ct) {
			t.Fatalf("%s: content type %q, want prefix %q", path, got, ct)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("%s: empty body", path)
		}
	}
}

func TestWithSpec(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.0"}`)
	h := swaggerui.Handler(swaggerui.WithSpec("openapi.json", spec))

	rec := get(t, h, "/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("spec: expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("spec: content type %q", ct)
	}
	if rec.Body.String() != string(spec) {
		t.Fatalf("spec body mismatch: %q", rec.Body.String())
	}
	if body := get(t, h, "/").Body.String(); !strings.Contains(body, `"url":"./openapi.json"`) {
		t.Fatalf("index does not load the spec: %s", body)
	}
}

func TestWithSpecYAMLContentType(t *testing.T) {
	h := swaggerui.Handler(swaggerui.WithSpec("openapi.yaml", []byte("openapi: 3.0.0")))
	if ct := get(t, h, "/openapi.yaml").Header().Get("Content-Type"); ct != "application/yaml" {
		t.Fatalf("yaml spec: content type %q", ct)
	}
}

func TestMultipleSpecsSelector(t *testing.T) {
	h := swaggerui.Handler(
		swaggerui.WithSpec("orders.json", []byte(`{}`)),
		swaggerui.WithSpecURL("/billing/openapi.json"),
	)
	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, `"urls":[`) {
		t.Fatalf("expected urls selector, got: %s", body)
	}
	if !strings.Contains(body, "/billing/openapi.json") {
		t.Fatalf("external spec URL missing: %s", body)
	}
}

func TestNotFoundAndMethods(t *testing.T) {
	h := swaggerui.Handler()
	if rec := get(t, h, "/nope.js"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path: expected 404, got %d", rec.Code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: expected 405, got %d", rec.Code)
	}
}

func TestNoTraversal(t *testing.T) {
	h := swaggerui.Handler()
	rec := get(t, h, "/../swaggerui.go")
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "package swaggerui") {
		t.Fatal("path traversal escaped the embedded assets")
	}
}

func TestMount(t *testing.T) {
	mux := http.NewServeMux()
	swaggerui.Mount(mux, "/swagger/", swaggerui.WithSpec("openapi.json", []byte(`{}`)))

	for _, path := range []string{"/swagger/", "/swagger/swagger-ui.css", "/swagger/openapi.json"} {
		if rec := get(t, mux, path); rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
	}
	// net/http redirects the bare prefix to the slashed form; the status
	// code differs across Go versions (301 before 1.26, 307 after).
	if rec := get(t, mux, "/swagger"); rec.Code < 300 || rec.Code >= 400 {
		t.Fatalf("/swagger: expected redirect, got %d", rec.Code)
	}
}

func TestMountRoot(t *testing.T) {
	mux := http.NewServeMux()
	swaggerui.Mount(mux, "/")
	if rec := get(t, mux, "/"); rec.Code != http.StatusOK {
		t.Fatalf("root mount: expected 200, got %d", rec.Code)
	}
}

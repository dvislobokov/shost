// Example: bundled Swagger UI and custom health probe paths.
//
// shost/swaggerui embeds swagger-ui-dist into the binary — the UI works
// offline, needs no CDN and no middleware. The OpenAPI document is
// embedded next to the code and served by the same handler. Health
// probes are mounted on custom paths (/live, /ready) instead of the
// default /healthz and /readyz. Standard library only.
//
//	go run .            # then open http://localhost:8080/swagger/
package main

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/shost/health"
	"github.com/dvislobokov/shost/httpsvc"
	"github.com/dvislobokov/shost/swaggerui"
)

//go:embed openapi.json
var spec []byte

func main() {
	log := shost.SlogLogger(slog.Default())

	mux := http.NewServeMux()
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "status": "shipped"},
			{"id": 2, "status": "pending"},
		})
	})

	// The UI at /swagger/, loading the spec the handler itself serves at
	// /swagger/openapi.json.
	swaggerui.Mount(mux, "/swagger/",
		swaggerui.WithSpec("openapi.json", spec),
		swaggerui.WithTitle("Orders API"),
	)

	// Probe paths overridden from the /healthz + /readyz defaults.
	reg := health.NewRegistry()
	reg.Mount(mux,
		health.WithLivePath("/live"),
		health.WithReadyPath("/ready"),
	)

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	host := shost.New().
		WithLogger(log).
		WithShutdownTimeout(15 * time.Second).
		AddService(httpsvc.New(addr, mux, httpsvc.WithName("orders-api"))).
		OnStarted(func() { reg.SetReady(true) }).
		OnStopping(func() { reg.SetReady(false) }).
		MustBuild()

	if err := host.Run(); err != nil {
		os.Exit(1)
	}
}

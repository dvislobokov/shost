// Example: Echo web framework hosted by shost.
//
// *echo.Echo implements http.Handler, so it plugs straight into
// shost/httpsvc and gets readiness, graceful shutdown and lifecycle
// management for free. Alongside the API this host runs a periodic
// cron job and exposes health endpoints wired to the host lifecycle.
//
//	go run .            # then: curl localhost:8080/hello
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/shost/cron"
	"github.com/dvislobokov/shost/health"
	"github.com/dvislobokov/shost/httpsvc"
	"github.com/labstack/echo/v4"
)

func main() {
	log := newSlogAdapter()

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.GET("/hello", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"message": "hello from echo + shost"})
	})

	// Health endpoints as regular echo routes; readiness flips with the
	// host lifecycle below — Kubernetes stops routing traffic before the
	// server begins draining.
	reg := health.NewRegistry()
	e.GET("/healthz", echo.WrapHandler(reg.LiveHandler()))
	e.GET("/readyz", echo.WrapHandler(reg.ReadyHandler()))

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	host := shost.New().
		WithLogger(log).
		WithEnvironment(shost.EnvironmentFromEnv("")).
		WithShutdownTimeout(15 * time.Second).
		AddService(httpsvc.New(addr, e, httpsvc.WithName("echo-api"))).
		AddService(cron.Every("heartbeat", 10*time.Second, func(ctx context.Context) error {
			log.Information("heartbeat")
			return nil
		}, cron.RunImmediately(), cron.WithErrorHandler(func(err error) {
			log.Error(err, "heartbeat failed")
		}))).
		OnStarted(func() { reg.SetReady(true) }).
		OnStopping(func() { reg.SetReady(false) }).
		MustBuild()

	if err := host.Run(); err != nil {
		os.Exit(1)
	}
}

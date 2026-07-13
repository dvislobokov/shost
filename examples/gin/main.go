// Example: Gin web framework hosted by shost.
//
// *gin.Engine implements http.Handler, so it plugs straight into
// shost/httpsvc. Alongside the API this host runs a supervised
// background worker: when the worker crashes, shost restarts it with
// exponential backoff instead of killing the process.
//
//	go run .            # then: curl localhost:8080/hello
package main

import (
	"net/http"
	"os"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/shost/health"
	"github.com/dvislobokov/shost/httpsvc"
	"github.com/gin-gonic/gin"
)

func main() {
	log := newSlogAdapter()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/hello", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello from gin + shost"})
	})

	// Health endpoints as regular gin routes; readiness flips with the
	// host lifecycle below.
	reg := health.NewRegistry()
	r.GET("/healthz", gin.WrapH(reg.LiveHandler()))
	r.GET("/readyz", gin.WrapH(reg.ReadyHandler()))

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	host := shost.New().
		WithLogger(log).
		WithEnvironment(shost.EnvironmentFromEnv("")).
		WithShutdownTimeout(15*time.Second).
		AddService(httpsvc.New(addr, r, httpsvc.WithName("gin-api"))).
		AddService(&queueWorker{log: log}, shost.WithRestart(shost.RestartPolicy{
			MaxAttempts:  5,
			InitialDelay: time.Second,
			MaxDelay:     30 * time.Second,
		})).
		OnStarted(func() { reg.SetReady(true) }).
		OnStopping(func() { reg.SetReady(false) }).
		MustBuild()

	if err := host.Run(); err != nil {
		os.Exit(1)
	}
}

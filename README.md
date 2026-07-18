# shost

[![CI](https://github.com/dvislobokov/shost/actions/workflows/ci.yml/badge.svg)](https://github.com/dvislobokov/shost/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dvislobokov/shost.svg)](https://pkg.go.dev/github.com/dvislobokov/shost)
[![Go Report Card](https://goreportcard.com/badge/github.com/dvislobokov/shost)](https://goreportcard.com/report/github.com/dvislobokov/shost)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Hosting framework for Go, inspired by `Microsoft.Extensions.Hosting`. Part of the
`s*` family: [sconf](https://dvislobokov.github.io/docs/) (configuration),
[srog](https://dvislobokov.github.io/docs/) (logging), sorm (ORM).

shost removes the `main()` boilerplate of long-running services: signal handling,
ordered startup, graceful shutdown with a deadline, and panic recovery — while
keeping dependency wiring explicit and idiomatic (no DI container).

```
go get github.com/dvislobokov/shost
```

## Quick start

```go
package main

import (
	"context"
	"os"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/srog"
)

type Worker struct{}

func (w *Worker) Name() string { return "worker" }

func (w *Worker) Start(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err() // graceful exit
		case <-ticker.C:
			// do work
		}
	}
}

func (w *Worker) Stop(ctx context.Context) error {
	// flush buffers, close connections; ctx carries the shutdown deadline
	return nil
}

func main() {
	log := srog.MustNew(srog.WithConsole())
	defer log.Close()

	host := shost.New().
		WithLogger(log). // *srog.Logger satisfies shost.Logger directly
		WithShutdownTimeout(30 * time.Second).
		WithStartTimeout(10 * time.Second).
		AddService(&Worker{}, shost.WithRestart(shost.RestartPolicy{MaxAttempts: 5})).
		OnStarted(func() { log.Information("app is up") }).
		MustBuild()

	if err := host.Run(); err != nil {
		os.Exit(1)
	}
}
```

`host.Run()` blocks until SIGINT/SIGTERM (or `host.Shutdown()`), then stops all
services in reverse registration order within the shutdown timeout.

## Semantics

- **Service contract** — `Start(ctx)` blocks for the lifetime of the service and
  returns after ctx is canceled; `Stop(ctx)` performs graceful shutdown under the
  shared deadline. For simple loops use `shost.ServiceFunc(name, run)`.
- **Startup** — services are launched in registration order.
- **Shutdown** — reverse order: each service's context is canceled, `Stop` is
  called, and the host waits for `Start` to return — all bounded by
  `WithShutdownTimeout` (default 30s). A stuck service is reported and abandoned
  instead of hanging the process.
- **Failure** — a service returning from `Start` before shutdown (with or without
  an error) stops the whole host; `Run` returns a non-nil error naming the service.
- **Panics** in `Start`/`Stop` are recovered, logged with a stack trace, and
  treated as service errors.
- **Restart policies** — `shost.WithRestart(shost.RestartPolicy{...})` supervises
  a service: premature exits trigger restarts with exponential backoff
  (`InitialDelay`/`MaxDelay`/`Factor`, defaults 1s/1m/2.0); the attempt counter
  resets after `ResetAfter` of stable run; the host stops only when
  `MaxAttempts` is exhausted (0 = unlimited).
- **Readiness** — a service may implement `shost.Readier` (`Ready() <-chan
  struct{}`); the host then waits for the channel to close before launching the
  next service, bounded in total by `WithStartTimeout`.
- **Lifecycle hooks** — `OnStarted` (all services launched and ready),
  `OnStopping` (shutdown began), `OnStopped` (everything stopped) — the analog
  of `IHostApplicationLifetime`. Hook panics are recovered and logged.
- **Startup tasks** — `AddStartupTask(name, fn)` runs one-shot work (database
  migrations, cache warm-up) sequentially before any service starts. A failed
  or panicking task prevents startup and `Run` returns its error; a shutdown
  signal during a task cancels it and exits cleanly.
- **Reload** — `OnReload` hooks + `host.Reload()`: re-read configuration or
  rotate logs without a restart. `Run` maps SIGHUP to `Reload` on Unix-like
  systems; under the Windows SCM use the winsvc module
  (`sc control <name> paramchange`).
- **Logging** — the `shost.Logger` interface is signature-compatible with srog;
  without a logger the host is silent, errors are still returned from `Run`.
  For stdlib logging use `shost.SlogLogger(l)` — message templates are
  rendered and placeholders become slog attributes.

## Environments

`shost.EnvironmentFromEnv("")` reads `APP_ENVIRONMENT` (unset → `Production`).
Pass it to the builder via `WithEnvironment` and layer environment-specific
config with sconf:

```go
env := shost.EnvironmentFromEnv("")
cfg, err := sconf.Load[Config](
	sconf.New().
		AddYAMLFile("appsettings.yaml").
		AddYAMLFile("appsettings."+env.String()+".yaml", sconf.Optional()).
		AddEnvironmentVariables("APP_"),
	os.Args[1:],
)
```

## Subpackages

- **`shost/httpsvc`** — `net/http` server as a Service: readiness once the
  listener accepts, in-flight requests drained on shutdown under the host
  deadline, forceful close when the deadline expires.

  ```go
  AddService(httpsvc.New(":8080", mux, httpsvc.WithName("api")))
  ```

- **`shost/cron`** — periodic jobs (timed BackgroundService). Runs never
  overlap; job errors and panics go to `WithErrorHandler` and the schedule
  continues, unless `StopOnError()` is set. Fixed intervals via `Every`,
  cron expressions via `At` + `Expr`/`MustExpr` (standard 5 fields, names,
  steps, `@daily`-style aliases); `WithJitter` spreads simultaneous runs
  across instances, `WithRunTimeout` bounds a single run's context.

  ```go
  AddService(cron.Every("cleanup", time.Hour, cleanupJob, cron.RunImmediately()))
  AddService(cron.At("backup", cron.MustExpr("0 3 * * *"), backupJob,
  	cron.WithJitter(time.Minute), cron.WithRunTimeout(30*time.Minute)))
  ```

- **`shost/health`** — `Checker` registry with `/healthz` (liveness) and
  `/readyz` (readiness) handlers for Kubernetes probes (paths overridable
  via `health.WithLivePath` / `health.WithReadyPath`). Readiness is wired
  to the host lifecycle:

  ```go
  reg := health.NewRegistry(health.CheckerFunc("db", db.Ping))
  reg.Mount(mux) // or reg.Mount(mux, health.WithReadyPath("/ready"))
  host := shost.New().
  	OnStarted(func() { reg.SetReady(true) }).
  	OnStopping(func() { reg.SetReady(false) }).
  	AddService(httpsvc.New(":8080", mux)).
  	MustBuild()
  ```

- **`shost/swaggerui`** — a fully bundled Swagger UI as a plain
  `http.Handler`: assets embedded via `go:embed`, works offline, no
  middleware, standard library only. Mount-point relative, so any prefix
  works:

  ```go
  swaggerui.Mount(mux, "/swagger/",
  	swaggerui.WithSpec("openapi.json", specBytes), // or WithSpecURL("/api/openapi.json")
  	swaggerui.WithTitle("Billing API"))
  ```

- **`shost/shosttest`** — test helpers: `shosttest.Start(t, builder)` runs the
  host inside a test, blocks until started, and registers a cleanup;
  `Stop`/`Wait` return the `Run` error for assertions. `shosttest.NewRecorder()`
  is an `Observer` that records lifecycle events (`rec.Has`, `rec.WaitFor`).

- **`shost/sdnotify`** — systemd `Type=notify` integration: `READY=1` on
  `OnStarted`, `STOPPING=1` on `OnStopping`, watchdog keep-alives when the
  unit sets `WatchdogSec=`, plus a unit-file generator (`sdnotify.Unit`).
  One call wires everything and is a no-op outside systemd:

  ```go
  host := sdnotify.Bind(shost.New().AddService(...)).MustBuild()
  ```

- **`shost/single`** — single-instance guarantee for agents: `flock` on
  Unix-like systems, an exclusive file handle on Windows; the OS releases
  the lock even on a crash, so there is no stale-pidfile handling.

  ```go
  lock, err := single.Acquire(single.DefaultPath("my-agent"))
  if errors.Is(err, single.ErrAlreadyRunning) { os.Exit(1) }
  defer lock.Release()
  ```

The core module and all subpackages above depend only on the standard library.

## Windows service (separate module)

**`github.com/dvislobokov/shost/winsvc`** — the analog of
`Microsoft.Extensions.Hosting.WindowsServices`. A Go binary started by the
Service Control Manager must speak the SCM protocol (signals never arrive);
`winsvc.Run` bridges it to the host lifecycle: START_PENDING during startup
tasks and readiness, RUNNING after `OnStarted`, graceful shutdown with
STOP_PENDING checkpoints on Stop/Shutdown controls, `Host.Reload` on
PARAMCHANGE, and startup errors in the Event Log. Outside SCM it falls back
to `host.Run()`, so the same binary works in a terminal and as a service.

```go
func main() {
	b := shost.New().
		AddService(&Collector{}).
		OnReload(reloadConfig)
	if err := winsvc.Run(b, winsvc.WithName("my-agent")); err != nil {
		os.Exit(1)
	}
}
```

`winsvc.Install`/`winsvc.Uninstall` register the service (auto or delayed
start) and its Event Log source — wire them to `install`/`uninstall` CLI
flags run elevated.

## gRPC (separate modules)

- **`github.com/dvislobokov/shost/grpcsvc`** — `*grpc.Server` as a Service:
  readiness once the listener accepts, `GracefulStop` under the host deadline,
  forceful `Stop` when the deadline expires.

  ```go
  AddService(grpcsvc.New(":9090", grpcServer, grpcsvc.WithName("grpc")))
  ```

- **`github.com/dvislobokov/shost/grpcgw`** — grpc-gateway (REST → gRPC
  transcoding) server as a Service: owns the `runtime.ServeMux`, the client
  connection to the gRPC endpoint and handler registration. Pass the
  protoc-generated `RegisterXxxHandler` functions:

  ```go
  AddService(grpcgw.New(":8081", "localhost:9090",
  	grpcgw.Register(pb.RegisterGreeterHandler),
  	grpcgw.WithHandler(corsMiddleware), // optional HTTP middleware
  ))
  ```

## Observability

The core exposes lifecycle events through `shost.Observer` — a struct of
optional callbacks in the style of `httptrace.ClientTrace`
(`HostStarted/HostStopped`, `ServiceStarted/Ready/Restarting/Stopped/Failed`),
registered via `WithObserver`. Any telemetry stack can hook in without adding
dependencies to the core.

The separate module **`github.com/dvislobokov/shost/otel`** maps these events
to OpenTelemetry: gauge `shost.host.up`, counters `shost.service.restarts` and
`shost.service.failures`, histogram `shost.service.stop.duration`, and a
`shost.service.stop` span per service shutdown.

```go
metricsHandler, provider, _ := shostotel.NewPrometheusHandler()
obs, _ := shostotel.NewObserver(shostotel.WithMeterProvider(provider))
mux.Handle("/metrics", metricsHandler)

host := shost.New().
	WithObserver(obs).
	OnStopped(func() { provider.Shutdown(context.Background()) }).
	AddService(httpsvc.New(":8080", mux)).
	MustBuild()
```

## Examples

Standalone runnable examples with popular web frameworks (both implement
`http.Handler`, so they plug into `httpsvc` directly) live in
[examples/](examples/): [Echo](examples/echo/) with a cron heartbeat and
health endpoints, and [Gin](examples/gin/) with a supervised background
worker demonstrating restart backoff.

## Roadmap

See [PLAN.md](PLAN.md): lifecycle events and restart policies (v0.2),
environments + sconf integration, `httpsvc`/`cron` adapters, health checks
(v0.3), OpenTelemetry metrics and tracing (v0.4), startup tasks,
cron expressions, slog adapter, `grpcsvc`/`grpcgw`, `shosttest` (v0.5),
daemon support: reload, `sdnotify`, `single`, `winsvc` (v0.6).

## License

[MIT](LICENSE) — covers the core and all modules in this repository.

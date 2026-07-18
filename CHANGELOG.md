# Changelog

All notable changes to shost are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Go modules in this repository are versioned together: the core
`github.com/dvislobokov/shost` and the separate modules `shost/otel`,
`shost/grpcsvc` and `shost/grpcgw` share the same version number.

## [1.0.1] — 2026-07-18

### Added

- **`examples/swagger`** — a runnable, standard-library-only example of
  the bundled Swagger UI: an embedded `openapi.json` served at
  `/swagger/` via `shost/swaggerui`, with health probes mounted on
  custom paths (`/live`, `/ready`).

## [1.0.0] — 2026-07-18

First stable release. The public API of the core module and the
`otel`, `grpcsvc`, `grpcgw` and `winsvc` modules is now covered by
semantic-versioning compatibility guarantees.

### Added

- **`shost/swaggerui`** — a fully bundled Swagger UI as a plain
  `http.Handler`: the swagger-ui-dist assets (5.32.9) are embedded via
  `go:embed`, so the UI works offline with no CDN and no middleware.
  `Handler(opts...)` is mount-point relative (works under any
  `http.StripPrefix`); `Mount(mux, "/swagger/", opts...)` wires it in one
  call. Options: `WithSpec(name, bytes)` serves the OpenAPI document from
  the handler itself (JSON/YAML content type by extension; several specs
  become a selector), `WithSpecURL(url)` points the UI at a document
  served elsewhere, `WithTitle` sets the page title. Standard library
  only, like the rest of the core module.
- **`health`: overridable probe paths** — `Registry.Mount` now accepts
  options: `health.WithLivePath("/live")` and
  `health.WithReadyPath("/ready")` override the default `/healthz` and
  `/readyz`. The no-option call is unchanged.

## [0.6.2] — 2026-07-15

### Added

- **MIT license** — the repository now ships a LICENSE file covering the
  core and all modules. All modules are re-tagged so their zips include
  it (pkg.go.dev only renders documentation for modules with a detected
  redistributable license).

## [0.6.1] — 2026-07-14

### Changed

- **Minimum Go version of the core module lowered to 1.22** (was 1.24).
  The core and its subpackages are standard-library-only; the newest
  features they use are `math/rand/v2` (Go 1.22) and `log/slog` (Go 1.21).
  A CI job now tests the core on the floor version. The `otel`, `grpcsvc`,
  `grpcgw` and `winsvc` modules stay on Go 1.25 — their dependencies
  (grpc, grpc-gateway, opentelemetry, x/sys) require it.

## [0.6.0] — 2026-07-14

Daemon-ready: running shost applications as system services (systemd,
Windows SCM) — the foundation for building system agents.

### Added

- **Reload** — `Builder.OnReload(fn)` hooks and `Host.Reload()`: re-read
  configuration or rotate logs without a restart. `Run` maps SIGHUP to
  `Reload` on Unix-like systems; concurrent reloads are serialized and hook
  panics are recovered. New `Host.ShutdownTimeout()` accessor for
  service-manager adapters.
- **`sdnotify` package** — systemd `Type=notify` integration, standard
  library only: `Ready`/`Stopping`/`Status`/`Watchdog` notifications,
  `WatchdogEnabled` (`WATCHDOG_USEC`/`WATCHDOG_PID`), abstract-socket
  support, and `Bind(builder)` wiring `READY=1` to `OnStarted`,
  `STOPPING=1` to `OnStopping` and a watchdog keep-alive service when the
  unit sets `WatchdogSec=`. No-op outside systemd. `Unit(cfg)` generates a
  `Type=notify` unit file for installers.
- **`single` package** — single-instance guarantee, standard library only:
  `Acquire(path)` takes a machine-wide lock (`flock` on Unix-like systems,
  an exclusive file handle on Windows) released by the OS even on a crash;
  returns `ErrAlreadyRunning` when another process holds it.
- **`winsvc` module** — `github.com/dvislobokov/shost/winsvc`, the analog
  of `Microsoft.Extensions.Hosting.WindowsServices`: `winsvc.Run(builder)`
  runs the host under SCM control when started as a Windows service
  (START_PENDING during startup, RUNNING after `OnStarted`, STOP_PENDING
  with advancing checkpoints while services drain, PARAMCHANGE →
  `Host.Reload`, startup/shutdown errors in the Event Log) and falls back
  to `Host.Run` in a terminal or on other platforms.
  `Install`/`Uninstall` register the service (automatic, delayed or manual
  start) and its Event Log source.

### Changed

- The `otel` module now requires a tagged core version instead of a local
  `replace` directive.

## [0.5.0] — 2026-07-14

Developer experience and gRPC.

### Added

- **Startup tasks** — `Builder.AddStartupTask(name, fn)` registers one-shot
  work (database migrations, cache warm-up) that runs sequentially before any
  service starts. A failed or panicking task prevents startup and `Run`
  returns its error; a shutdown signal during a task cancels its context and
  exits cleanly.
- **`cron`: cron expressions** — the `Schedule` interface (`Next(after)`),
  `Expr`/`MustExpr` parsing standard 5-field expressions (lists, ranges,
  steps, `jan`/`mon` names, `@hourly`…`@yearly` aliases, classic
  day-of-month OR day-of-week rule) and the `At(name, schedule, job)`
  constructor. A schedule that will never fire again parks the service until
  shutdown instead of stopping the host. Standard library only.
- **`cron`: run options** — `WithJitter(d)` delays each run by a random
  duration in `[0, d)` to spread simultaneous runs across instances;
  `WithRunTimeout(d)` bounds a single run's context. Both work with `Every`
  and `At`.
- **slog adapter** — `shost.SlogLogger(*slog.Logger)` adapts the standard
  library logger to the `shost.Logger` interface: srog-style message
  templates are rendered into the message and each `{Placeholder}`
  additionally becomes a slog attribute. The core stays dependency-free.
- **`shosttest` package** — test helpers: `Start(t, builder)` runs the host
  inside a test, blocks until all services are started and ready, and
  registers an automatic cleanup; `Stop`/`Wait` return the `Run` error for
  assertions. `NewRecorder()` provides a ready-made `Observer` that records
  lifecycle events, with `Has` and polling `WaitFor` helpers.
- **`grpcsvc` module** — `github.com/dvislobokov/shost/grpcsvc` runs a
  `*grpc.Server` as a `shost.Service`: readiness once the listener accepts,
  `Addr()` for `:0` listeners, `GracefulStop` under the host's shutdown
  deadline and a forceful `Stop` when the deadline expires.
- **`grpcgw` module** — `github.com/dvislobokov/shost/grpcgw` runs a
  grpc-gateway (REST → gRPC transcoding) HTTP server as a `shost.Service`.
  It owns the gateway boilerplate: `runtime.ServeMux` construction, the
  client connection to the gRPC endpoint (plaintext by default, configurable
  via `WithDialOptions`) and registration of protoc-generated
  `RegisterXxxHandler` functions via `grpcgw.Register`. HTTP middleware via
  `WithHandler`, server tuning via `WithServer`, mux options via
  `WithServeMuxOptions`.
- **Examples** — standalone runnable examples in `examples/`: Echo with a
  cron heartbeat and health endpoints, Gin with a supervised background
  worker demonstrating restart backoff.

## [0.4.0] — 2026-07-13

Observability.

### Added

- **`shost.Observer`** — lifecycle events as a struct of optional callbacks
  in the style of `httptrace.ClientTrace` (`HostStarted`, `HostStopped`,
  `ServiceStarted`, `ServiceReady`, `ServiceRestarting`, `ServiceStopped`,
  `ServiceFailed`), registered via `Builder.WithObserver`. Multiple
  observers run in registration order; callback panics are recovered and
  logged. The core stays dependency-free.
- **`otel` module** — `github.com/dvislobokov/shost/otel` maps Observer
  events to OpenTelemetry: gauge `shost.host.up`, counters
  `shost.service.restarts` and `shost.service.failures`, histogram
  `shost.service.stop.duration`, and a `shost.service.stop` span per service
  shutdown. `NewPrometheusHandler()` returns a ready `/metrics` handler.

## [0.3.0] — 2026-07-13

Ecosystem: environments and the first adapter subpackages (standard library
only).

### Added

- **Environments** — `shost.Environment` (`Development`/`Staging`/
  `Production` or custom), `EnvironmentFromEnv` reading `APP_ENVIRONMENT`,
  `Builder.WithEnvironment`, `Host.Environment()`; layering
  `appsettings.{env}.yaml` with sconf documented as a pattern, without a
  dependency.
- **`httpsvc`** — `net/http` server as a Service: readiness once the
  listener accepts (`shost.Readier`), `Addr()` for `:0` listeners, in-flight
  requests drained on shutdown under the host deadline, forceful close when
  the deadline expires.
- **`cron`** — periodic jobs (timed BackgroundService): `Every(name,
  interval, job)`, non-overlapping runs, `RunImmediately`, `StopOnError`,
  `WithErrorHandler` receiving job errors and recovered panics.
- **`health`** — `Checker` registry with JSON `/healthz` (liveness) and
  `/readyz` (readiness) handlers for Kubernetes probes; readiness wired to
  the host lifecycle via `SetReady` from `OnStarted`/`OnStopping` hooks.

## [0.2.0] — 2026-07-13

Lifecycle: hooks, supervision and readiness.

### Added

- **Lifecycle hooks** — `OnStarted` (all services launched and ready),
  `OnStopping` (shutdown began), `OnStopped` (everything stopped) — the
  analog of `IHostApplicationLifetime`. Hook panics are recovered and logged.
- **Restart policies** — `shost.WithRestart(shost.RestartPolicy{...})`
  supervises a service: premature exits trigger restarts with exponential
  backoff (`InitialDelay`/`MaxDelay`/`Factor`), the attempt counter resets
  after `ResetAfter` of stable run, and the host stops only when
  `MaxAttempts` is exhausted (0 = unlimited).
- **Readiness** — the optional `shost.Readier` interface (`Ready() <-chan
  struct{}`): the host waits for the channel to close before launching the
  next service, bounded in total by `Builder.WithStartTimeout`.

## [0.1.0] — 2026-07-13

Initial release: the core hosting framework, standard library only.

### Added

- **Service contract** — `Service` (`Name`/`Start`/`Stop`) with blocking
  `Start` semantics and `Stop` under the shared shutdown deadline;
  `ServiceFunc` for simple loops.
- **Builder and Host** — fluent `shost.New()` builder with accumulated
  configuration errors (`Build`/`MustBuild`), `WithLogger`,
  `WithShutdownTimeout`.
- **Run loop** — `Run` blocks until SIGINT/SIGTERM, programmatic
  `Shutdown()` or a service exiting on its own; `RunContext` for a
  caller-provided shutdown trigger. Services start in registration order and
  stop in reverse order within the shutdown timeout; a stuck service is
  reported and abandoned instead of hanging the process.
- **Failure semantics** — a service returning from `Start` before shutdown
  (with or without an error) stops the whole host; panics in `Start`/`Stop`
  are recovered, logged with a stack trace, and treated as service errors.
- **Logging** — the minimal `shost.Logger` interface,
  signature-compatible with srog; without a logger the host is silent while
  errors are still returned from `Run`.

[0.6.2]: https://github.com/dvislobokov/shost/compare/v0.6.1...v0.6.2
[0.6.1]: https://github.com/dvislobokov/shost/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/dvislobokov/shost/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/dvislobokov/shost/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/dvislobokov/shost/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/dvislobokov/shost/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/dvislobokov/shost/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/dvislobokov/shost/releases/tag/v0.1.0

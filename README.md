# shost

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
- **Logging** — the `shost.Logger` interface is signature-compatible with srog;
  without a logger the host is silent, errors are still returned from `Run`.

## Roadmap

See [PLAN.md](PLAN.md): lifecycle events and restart policies (v0.2),
environments + sconf integration, `httpsvc`/`cron` adapters, health checks
(v0.3), OpenTelemetry metrics and tracing (v0.4).

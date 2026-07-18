# shost examples

Each example is a standalone Go module (frameworks stay out of the core
dependencies). Run with `go run .` inside the example directory; the port
defaults to `:8080` and can be overridden with the `PORT` env variable.

- **[echo](echo/)** — Echo API hosted via `shost/httpsvc` (readiness, graceful
  shutdown), a periodic `shost/cron` heartbeat, and `shost/health` endpoints
  wired to the host lifecycle.
- **[gin](gin/)** — Gin API plus a supervised background worker: it crashes on
  purpose every third batch and `shost.WithRestart` restarts it with
  exponential backoff instead of killing the process.
- **[swagger](swagger/)** — bundled Swagger UI (`shost/swaggerui`) serving an
  embedded OpenAPI document at `/swagger/`, with health probes mounted on
  custom paths (`/live`, `/ready`). Standard library only.

```
cd examples/echo   # or examples/gin
go run .
curl localhost:8080/hello
curl localhost:8080/readyz
```

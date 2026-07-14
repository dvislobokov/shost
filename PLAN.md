# shost — план библиотеки

Хостинг-фреймворк для Go в духе `Microsoft.Extensions.Hosting`, продолжение семейства
`s*`: [sconf](https://dvislobokov.github.io/docs/) (конфигурация), [srog](https://dvislobokov.github.io/docs/) (логирование), sorm (ORM).

## Философия

- Убрать boilerplate из `main()`: сигналы, errgroup, порядок остановки, таймауты.
- Проверенные паттерны .NET Generic Host, но в идиомах Go: `context.Context` вместо
  `CancellationToken`, **без DI-контейнера** — зависимости собираются явно в `main()`.
- Первоклассная интеграция с sconf и srog, но без жёсткой связки: логгер и конфиг —
  интерфейсы, s*-библиотеки подключаются адаптерами по умолчанию.

## Ядро

### Контракт сервиса (аналог IHostedService / BackgroundService)

```go
type Service interface {
    Name() string
    Start(ctx context.Context) error // блокирующий; живёт до отмены ctx
    Stop(ctx context.Context) error  // graceful stop, ctx несёт дедлайн
}
```

- Утилита `shost.ServiceFunc(name, startFn)` для простых случаев без Stop.
- Panic внутри Start/Stop перехватывается, логируется, трактуется как ошибка сервиса.

### Builder и Host

```go
host := shost.New().
    WithConfig(cfg).                          // sconf.Config или интерфейс
    WithLogger(log).                          // srog или адаптер slog
    WithEnvironment(shost.FromEnv("APP_")).   // Development/Staging/Production
    WithShutdownTimeout(30 * time.Second).
    AddService(&Worker{}).
    AddService(httpsvc.New(":8080", mux)).
    OnStarted(func() { ... }).
    OnStopping(func() { ... }).
    MustBuild()

host.Run() // блокирует до SIGINT/SIGTERM или host.Shutdown(); возвращает error
```

### Lifecycle-семантика

- **Старт**: в порядке регистрации; опция `Parallel()` для независимых сервисов.
- **Остановка**: строго в обратном порядке (как в .NET), с общим shutdown timeout.
- **Ошибка/выход сервиса** → остановка всего хоста, ненулевой код выхода
  (аналог `BackgroundServiceExceptionBehavior.StopHost`).
- **Политики рестарта** (то, чего нет в .NET): `AddService(w, shost.Restart(shost.Backoff(...)))`.
- **События**: OnStarted / OnStopping / OnStopped + программный `host.Shutdown()`
  (аналог `IHostApplicationLifetime`).
- Лог каждого этапа: "service {Name} starting/started/stopping/stopped in {Elapsed}".

## Environments

- `shost.Environment` — Development / Staging / Production / кастомные, из
  `APP_ENVIRONMENT` (префикс настраиваемый).
- Хелпер для sconf: автодобавление `appsettings.{env}.yaml` поверх базового.
- `env.IsDevelopment()` и т.п. доступны сервисам через контекст или билдер.

## Подпакеты-адаптеры

| Пакет            | Назначение |
|------------------|------------|
| `shost/httpsvc`  | `net/http` сервер как Service: graceful `Shutdown()`, таймауты из коробки |
| `shost/cron`     | Периодические задачи (аналог timed BackgroundService): интервал/cron-выражение, overlap policy |
| `shost/grpcsvc`  | gRPC сервер как Service (позже, при потребности) |

## Health checks

- `shost/health`: интерфейс `Checker { Name() string; Check(ctx) error }`.
- Регистрация: `AddHealthCheck(...)`; сервисы могут сами реализовывать Checker.
- Готовый handler для httpsvc: `/healthz` (liveness) и `/readyz` (readiness —
  становится OK только после OnStarted, падает при OnStopping — важно для K8s).

## Наблюдаемость (метрики, трассировка)

- `shost/otel`: опциональная интеграция с OpenTelemetry — не тащить зависимость в ядро
  (отдельный go-модуль или build-tag-подход, решить при реализации).
- Метрики хоста: uptime, состояние сервисов (running/stopped/restarting), количество
  рестартов, длительность старта/остановки.
- Трассировка lifecycle: span на старт/стоп каждого сервиса.
- `/metrics` handler (Prometheus) как готовый чекпоинт в httpsvc.

## Этапы

1. ✅ **v0.1 — ядро**: Service, Builder, Run, сигналы, порядок старт/стоп, таймауты,
   panic recovery, лог lifecycle через srog. Тесты на порядок остановки, таймауты, сигналы.
2. ✅ **v0.2 — lifecycle+**: события Started/Stopping/Stopped, политики рестарта
   с backoff (WithRestart + RestartPolicy), интерфейс готовности Readier +
   WithStartTimeout — т.к. Start блокирующий, барьер «сервис готов» требует
   отдельного сигнала. (`Shutdown()` реализован уже в v0.1; параллельный старт
   отложен — с барьером готовности он нужен реже.)
3. ✅ **v0.3 — экосистема**: Environment (+WithEnvironment, EnvironmentFromEnv),
   `httpsvc`, `cron` (интервальные задачи; cron-выражения — позже), `health`
   с /healthz и /readyz. Интеграция со sconf — паттерном в доках, без
   зависимости (ядро остаётся zero-dependency).
4. ✅ **v0.4 — наблюдаемость**: shost.Observer в ядре (struct опциональных
   колбэков в стиле httptrace.ClientTrace, zero-dep) + отдельный модуль
   `shost/otel` (метрики, спаны остановки, Prometheus handler).
   ВАЖНО: в otel/go.mod стоит `replace github.com/dvislobokov/shost => ../` —
   убрать и заменить на требование тегнутой версии после первого релиза ядра.
5. ✅ **v0.5 — DX и gRPC**: startup tasks (AddStartupTask — миграции/прогрев
   до старта сервисов), cron-выражения (Schedule, Expr/MustExpr, 5 полей +
   алиасы) + WithJitter/WithRunTimeout, адаптер slog (shost.SlogLogger,
   zero-dep), пакет shosttest (Start/Stop/Wait + Recorder для Observer),
   отдельные модули `shost/grpcsvc` (grpc.Server как Service) и
   `shost/grpcgw` (grpc-gateway: ServeMux + client conn + регистрация
   хендлеров как Service).
6. **Docs**: страница shost на dvislobokov.github.io в общем стиле, README с
   примером «полный сервис на sconf+srog+shost за 30 строк».

## Открытые вопросы

- Module path: `github.com/dvislobokov/shost` (по аналогии с srog).
- Логгер в ядре: свой минимальный интерфейс + адаптеры srog/slog, или `*slog.Logger`
  напрямую? (склоняюсь к своему интерфейсу из 4–5 методов, srog — дефолт в доках).
- otel как отдельный go-модуль в том же репо (`shost/otel/go.mod`), чтобы ядро
  оставалось zero-dependency.
- Windows: помимо SIGINT/SIGTERM обработать `os.Interrupt`/CTRL_CLOSE корректно.

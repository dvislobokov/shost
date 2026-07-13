// Package shostotel maps shost lifecycle events to OpenTelemetry: metrics
// (host up, service restarts/failures, stop durations), spans for service
// shutdown, and a ready-made Prometheus handler.
//
//	obs, err := shostotel.NewObserver()
//	host := shost.New().WithObserver(obs)...
package shostotel

import (
	"context"
	"time"

	"github.com/dvislobokov/shost"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const scopeName = "github.com/dvislobokov/shost/otel"

type config struct {
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
}

// Option customizes NewObserver.
type Option func(*config)

// WithMeterProvider overrides the global otel meter provider.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *config) { c.meterProvider = mp }
}

// WithTracerProvider overrides the global otel tracer provider.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) { c.tracerProvider = tp }
}

// NewObserver creates a shost.Observer recording lifecycle telemetry:
//
//   - gauge shost.host.up — 1 while the host runs, 0 after stop
//   - counter shost.service.restarts — supervised restarts, by service
//   - counter shost.service.failures — exits that stop the host, by service
//   - histogram shost.service.stop.duration — graceful stop time, seconds
//   - span "shost.service.stop" per service shutdown, error status on failure
func NewObserver(opts ...Option) (shost.Observer, error) {
	cfg := config{
		meterProvider:  otel.GetMeterProvider(),
		tracerProvider: otel.GetTracerProvider(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	meter := cfg.meterProvider.Meter(scopeName)
	tracer := cfg.tracerProvider.Tracer(scopeName)

	hostUp, err := meter.Int64Gauge("shost.host.up",
		metric.WithDescription("1 while the host is running, 0 once stopped"))
	if err != nil {
		return shost.Observer{}, err
	}
	restarts, err := meter.Int64Counter("shost.service.restarts",
		metric.WithDescription("Supervised service restarts"))
	if err != nil {
		return shost.Observer{}, err
	}
	failures, err := meter.Int64Counter("shost.service.failures",
		metric.WithDescription("Service exits that stopped the host"))
	if err != nil {
		return shost.Observer{}, err
	}
	stopDuration, err := meter.Float64Histogram("shost.service.stop.duration",
		metric.WithDescription("Graceful stop duration per service"),
		metric.WithUnit("s"))
	if err != nil {
		return shost.Observer{}, err
	}

	ctx := context.Background()
	svc := func(name string) metric.MeasurementOption {
		return metric.WithAttributes(attribute.String("service", name))
	}

	return shost.Observer{
		HostStarted: func() {
			hostUp.Record(ctx, 1)
		},
		HostStopped: func(error) {
			hostUp.Record(ctx, 0)
		},
		ServiceRestarting: func(name string, attempt int, delay time.Duration, err error) {
			restarts.Add(ctx, 1, svc(name))
		},
		ServiceFailed: func(name string, err error) {
			failures.Add(ctx, 1, svc(name))
		},
		ServiceStopped: func(name string, elapsed time.Duration, err error) {
			stopDuration.Record(ctx, elapsed.Seconds(), svc(name))
			// The stop already happened; reconstruct its span from elapsed.
			_, span := tracer.Start(ctx, "shost.service.stop",
				trace.WithTimestamp(time.Now().Add(-elapsed)),
				trace.WithAttributes(attribute.String("service", name)))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		},
	}, nil
}

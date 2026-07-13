package shostotel

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// NewPrometheusHandler creates a MeterProvider backed by a dedicated
// Prometheus registry and an http.Handler serving it in Prometheus text
// format — a ready checkpoint for httpsvc:
//
//	handler, provider, err := shostotel.NewPrometheusHandler()
//	obs, _ := shostotel.NewObserver(shostotel.WithMeterProvider(provider))
//	mux.Handle("/metrics", handler)
//
// The caller owns the provider and should call provider.Shutdown on exit
// (e.g. from the host's OnStopped hook).
func NewPrometheusHandler() (http.Handler, *sdkmetric.MeterProvider, error) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, nil, err
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return handler, provider, nil
}

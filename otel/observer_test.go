package shostotel_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shostotel "github.com/dvislobokov/shost/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObserverRecordsMetricsAndSpans(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	obs, err := shostotel.NewObserver(
		shostotel.WithMeterProvider(mp),
		shostotel.WithTracerProvider(tp),
	)
	if err != nil {
		t.Fatalf("NewObserver failed: %v", err)
	}

	obs.HostStarted()
	obs.ServiceRestarting("worker", 1, time.Second, errors.New("crash"))
	obs.ServiceFailed("worker", errors.New("crash"))
	obs.ServiceStopped("worker", 20*time.Millisecond, nil)
	obs.HostStopped(nil)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	got := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			got[m.Name] = true
		}
	}
	for _, want := range []string{
		"shost.host.up",
		"shost.service.restarts",
		"shost.service.failures",
		"shost.service.stop.duration",
	} {
		if !got[want] {
			t.Errorf("metric %q was not recorded; got %v", want, got)
		}
	}

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "shost.service.stop" {
		t.Fatalf("expected one shost.service.stop span, got %v", spans)
	}
}

func TestPrometheusHandlerServesMetrics(t *testing.T) {
	handler, provider, err := shostotel.NewPrometheusHandler()
	if err != nil {
		t.Fatalf("NewPrometheusHandler failed: %v", err)
	}
	defer provider.Shutdown(context.Background())

	obs, err := shostotel.NewObserver(shostotel.WithMeterProvider(provider))
	if err != nil {
		t.Fatalf("NewObserver failed: %v", err)
	}
	obs.HostStarted()
	obs.ServiceRestarting("worker", 1, time.Second, errors.New("crash"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	text := string(body)
	if !strings.Contains(text, "shost_host_up") {
		t.Errorf("expected shost_host_up in output:\n%s", text)
	}
	if !strings.Contains(text, "shost_service_restarts") {
		t.Errorf("expected shost_service_restarts in output:\n%s", text)
	}
}

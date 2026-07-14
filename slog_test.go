package shost_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dvislobokov/shost"
)

func newTestSlog() (*bytes.Buffer, shost.Logger) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &buf, shost.SlogLogger(l)
}

func TestSlogLoggerRendersTemplate(t *testing.T) {
	buf, log := newTestSlog()
	log.Information("service {Service} stopped in {Elapsed}", "api", "12ms")

	out := buf.String()
	for _, want := range []string{
		`msg="service api stopped in 12ms"`,
		"Service=api",
		"Elapsed=12ms",
		"level=INFO",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSlogLoggerLevels(t *testing.T) {
	buf, log := newTestSlog()
	log.Debug("d")
	log.Information("i")
	log.Warning("w")
	log.Error(errors.New("boom"), "e")

	out := buf.String()
	for _, want := range []string{"level=DEBUG", "level=INFO", "level=WARN", "level=ERROR", "error=boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSlogLoggerUnmatchedPlaceholders(t *testing.T) {
	buf, log := newTestSlog()
	// More placeholders than args: the extra one stays verbatim.
	log.Information("a {X} b {Y}", 1)
	out := buf.String()
	if !strings.Contains(out, `msg="a 1 b {Y}"`) {
		t.Errorf("unexpected rendering:\n%s", out)
	}

	buf.Reset()
	// No placeholders at all.
	log.Information("plain message")
	if !strings.Contains(buf.String(), `msg="plain message"`) {
		t.Errorf("unexpected rendering:\n%s", buf.String())
	}
}

func TestSlogLoggerNilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil logger")
		}
	}()
	shost.SlogLogger(nil)
}

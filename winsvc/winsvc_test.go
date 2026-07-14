package winsvc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dvislobokov/shost"
)

func TestBuildOptionsDefaultsToExeName(t *testing.T) {
	o := buildOptions(`C:\Program Files\agent\my-agent.exe`, nil)
	if o.name != "my-agent" {
		t.Fatalf("wrong default name: %q", o.name)
	}
	o = buildOptions("/usr/local/bin/my-agent", []Option{WithName("custom")})
	if o.name != "custom" {
		t.Fatalf("WithName not applied: %q", o.name)
	}
}

// Run outside SCM must behave like Host.Run: here the host fails fast
// because its only service exits with an error.
func TestRunConsoleFallback(t *testing.T) {
	boom := errors.New("boom")
	b := shost.New().AddService(shost.ServiceFunc("fail", func(ctx context.Context) error {
		return boom
	}))
	if err := Run(b); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got: %v", err)
	}
}

func TestRunBuildError(t *testing.T) {
	b := shost.New().AddService(nil)
	err := Run(b)
	if err == nil || !strings.Contains(err.Error(), "nil service") {
		t.Fatalf("expected build error, got: %v", err)
	}
}

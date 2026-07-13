package shost_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dvislobokov/shost"
)

func nopService(name string) shost.Service {
	return shost.ServiceFunc(name, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
}

func TestBuildRejectsDuplicateNames(t *testing.T) {
	_, err := shost.New().
		AddService(nopService("worker")).
		AddService(nopService("worker")).
		Build()
	if err == nil || !strings.Contains(err.Error(), `duplicate service name "worker"`) {
		t.Fatalf("expected duplicate name error, got: %v", err)
	}
}

func TestBuildRejectsNilService(t *testing.T) {
	_, err := shost.New().AddService(nil).Build()
	if err == nil || !strings.Contains(err.Error(), "nil service") {
		t.Fatalf("expected nil service error, got: %v", err)
	}
}

func TestBuildRejectsEmptyName(t *testing.T) {
	_, err := shost.New().AddService(nopService("")).Build()
	if err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("expected empty name error, got: %v", err)
	}
}

func TestBuildRejectsInvalidTimeout(t *testing.T) {
	_, err := shost.New().WithShutdownTimeout(0).Build()
	if err == nil || !strings.Contains(err.Error(), "shutdown timeout must be positive") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestBuildRejectsNilLogger(t *testing.T) {
	_, err := shost.New().WithLogger(nil).Build()
	if err == nil || !strings.Contains(err.Error(), "nil logger") {
		t.Fatalf("expected nil logger error, got: %v", err)
	}
}

func TestBuildAccumulatesAllErrors(t *testing.T) {
	_, err := shost.New().
		AddService(nil).
		WithShutdownTimeout(-1).
		Build()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"nil service", "shutdown timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to mention %q, got: %v", want, err)
		}
	}
}

func TestMustBuildPanicsOnError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	shost.New().AddService(nil).MustBuild()
}

func TestEmptyHostRunsAndShutsDown(t *testing.T) {
	h := shost.New().MustBuild()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.RunContext(ctx); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

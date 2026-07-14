package shost_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/dvislobokov/shost"
)

func TestReloadRunsHooksInOrder(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		OnReload(func() { events.add("reload:1") }).
		OnReload(func() { events.add("reload:2") }).
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	wait := runHost(t, h, context.Background())
	waitFor(t, events, "start:a")

	h.Reload()
	h.Reload()

	h.Shutdown()
	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	got := events.list()
	want := []string{"start:a", "reload:1", "reload:2", "reload:1", "reload:2", "stop:a"}
	if len(got) != len(want) {
		t.Fatalf("wrong events:\n got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wrong events:\n got %v\nwant %v", got, want)
		}
	}
}

func TestReloadHookPanicIsRecovered(t *testing.T) {
	var after atomic.Bool
	h := shost.New().
		OnReload(func() { panic("reload boom") }).
		OnReload(func() { after.Store(true) }).
		AddService(shost.ServiceFunc("a", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})).
		MustBuild()

	wait := runHost(t, h, context.Background())
	h.Reload() // must not panic the caller
	if !after.Load() {
		t.Fatal("hook after the panicking one did not run")
	}
	h.Shutdown()
	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
}

func TestOnReloadNilHookFailsBuild(t *testing.T) {
	if _, err := shost.New().OnReload(nil).Build(); err == nil {
		t.Fatal("expected error for nil reload hook")
	}
}

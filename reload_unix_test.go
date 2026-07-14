//go:build unix

package shost_test

import (
	"syscall"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
)

func TestSighupTriggersReload(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		OnReload(func() { events.add("reload") }).
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	// Run (not RunContext) installs the SIGHUP handler.
	res := make(chan error, 1)
	go func() { res <- h.Run() }()
	waitFor(t, events, "start:a")

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitFor(t, events, "reload")

	h.Shutdown()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("expected clean shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host did not stop in time")
	}
}

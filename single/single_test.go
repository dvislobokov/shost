package single_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dvislobokov/shost/single"
)

func TestAcquireReleaseReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.lock")

	lock, err := single.Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	if lock.Path() != path {
		t.Fatalf("wrong path: %s", lock.Path())
	}

	if _, err := single.Acquire(path); !errors.Is(err, single.ErrAlreadyRunning) {
		t.Fatalf("second Acquire: expected ErrAlreadyRunning, got: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	lock2, err := single.Acquire(path)
	if err != nil {
		t.Fatalf("re-Acquire after release failed: %v", err)
	}
	if err := lock2.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestAcquireAcrossProcesses verifies the lock holds against a different
// process, not just another descriptor in this one.
func TestAcquireAcrossProcesses(t *testing.T) {
	if os.Getenv("SINGLE_TEST_CHILD") != "" {
		_, err := single.Acquire(os.Getenv("SINGLE_TEST_CHILD"))
		if errors.Is(err, single.ErrAlreadyRunning) {
			os.Exit(3)
		}
		os.Exit(0)
	}

	path := filepath.Join(t.TempDir(), "agent.lock")
	lock, err := single.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	cmd := exec.Command(os.Args[0], "-test.run", "TestAcquireAcrossProcesses")
	cmd.Env = append(os.Environ(), "SINGLE_TEST_CHILD="+path)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("child should have exited with 3 (already running), got err=%v out=%s", err, strings.TrimSpace(string(out)))
	}
}

func TestDefaultPath(t *testing.T) {
	p := single.DefaultPath("my-agent")
	if !strings.HasSuffix(p, "my-agent.lock") {
		t.Fatalf("unexpected path: %s", p)
	}
}

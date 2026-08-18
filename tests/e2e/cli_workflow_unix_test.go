//go:build linux || darwin

package e2e

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestManagedValidationTimeoutTerminatesProcessTree(t *testing.T) {
	env := newEnvironment(t, true)
	env.runJSON(t, "init", "--json")
	env.runJSON(t, "task", "create", "--title", "Timeout process tree", "--json")
	claimed := env.runJSONVersion(t, "2", "task", "claim", "1", "--json")
	run := object(t, claimed, "result")
	runID, leaseID := stringValue(t, run, "id"), stringValue(t, run, "lease_id")
	pidFile := filepath.Join(env.root, "child.pid")

	result := env.runJSONVersion(t, "2",
		"run", "validate", runID,
		"--lease", leaseID,
		"--timeout", "150ms",
		"--termination-grace", "50ms",
		"--json",
		"--", "/bin/sh", "-c", "sleep 30 & echo $! > child.pid; wait",
	)
	execution := object(t, result, "result", "execution")
	validation := object(t, result, "result", "validation")
	if stringValue(t, execution, "status") != "failed" || !boolValue(t, execution, "timed_out") || stringValue(t, validation, "status") != "failed" {
		t.Fatalf("timeout result = %#v", result)
	}

	pid := writePID(t, pidFile)
	deadline := time.Now().Add(time.Second)
	for processExists(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processExists(pid) {
		t.Fatalf("timed-out child process %d still exists", pid)
	}
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func writePID(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read child PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid <= 0 {
		t.Fatalf("parse child PID %q: %v", content, err)
	}

	return pid
}

//go:build linux || darwin

package executor

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecuteTimeoutTerminatesProcessGroup(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Execute(context.Background(), Request{
		Argv:             helperArgv("tree"),
		Env:              helperEnv(),
		Stdout:           &stdout,
		Timeout:          100 * time.Millisecond,
		TerminationGrace: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("result = %+v, want timeout", result)
	}

	var pid int
	for _, line := range strings.Split(stdout.String(), "\n") {
		if value, ok := strings.CutPrefix(line, "child="); ok {
			pid, _ = strconv.Atoi(value)
		}
	}
	if pid == 0 {
		t.Fatalf("child PID not found in stdout %q", stdout.String())
	}

	deadline := time.Now().Add(time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		if err != nil {
			t.Fatalf("checking child %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived process group termination", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestExecuteExitStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     string
		exitCode int
	}{
		{name: "success", mode: "success", exitCode: 0},
		{name: "failure", mode: "failure", exitCode: 7},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			result, err := Execute(context.Background(), Request{
				Argv:   helperArgv(test.mode),
				Env:    helperEnv(),
				Stdout: &stdout,
			})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.ExitCode != test.exitCode {
				t.Errorf("ExitCode = %d, want %d", result.ExitCode, test.exitCode)
			}
			if got := stdout.String(); got != "stdout\n" {
				t.Errorf("stdout = %q, want %q", got, "stdout\n")
			}
			if result.StartedAt.IsZero() || result.FinishedAt.IsZero() {
				t.Error("timestamps must be populated")
			}
		})
	}
}

func TestExecuteTimeout(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), Request{
		Argv:             helperArgv("sleep"),
		Env:              helperEnv(),
		Timeout:          30 * time.Millisecond,
		TerminationGrace: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.TimedOut || result.Cancelled {
		t.Errorf("result = %+v, want timed out only", result)
	}
}

func TestExecuteCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Execute(ctx, Request{Argv: helperArgv("sleep"), Env: helperEnv()})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Cancelled || result.TimedOut {
		t.Errorf("result = %+v, want cancelled only", result)
	}
}

func TestExecuteStartFailure(t *testing.T) {
	t.Parallel()

	if _, err := Execute(context.Background(), Request{}); err == nil {
		t.Fatal("Execute() error = nil, want empty argv error")
	}
	if _, err := Execute(context.Background(), Request{Argv: []string{"definitely-not-an-istok-executable"}}); err == nil {
		t.Fatal("Execute() error = nil, want start error")
	}
	if _, err := Execute(context.Background(), Request{Argv: helperArgv("success"), Dir: t.TempDir() + "/missing"}); err == nil {
		t.Fatal("Execute() error = nil, want setup error for missing directory")
	}
}

func TestFilterEnvironment(t *testing.T) {
	t.Parallel()

	got := FilterEnvironment([]string{
		"SECRET=no",
		"PATH=/base",
		"LC_ALL=C",
		"HOME=/home/base",
	}, map[string]string{"PATH": "/override", "ZED": "last", "AAA": "first"})
	want := []string{"LC_ALL=C", "HOME=/home/base", "AAA=first", "PATH=/override", "ZED=last"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("FilterEnvironment() = %v, want %v", got, want)
	}
}

func helperArgv(mode string) []string {
	return []string{os.Args[0], "-test.run=^TestExecutorHelperProcess$", "--", mode}
}

func helperEnv() []string {
	return append(os.Environ(), "GO_WANT_EXECUTOR_HELPER=1")
}

func TestExecutorHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_EXECUTOR_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	fmt.Fprintln(os.Stdout, "stdout")
	switch mode {
	case "success":
		os.Exit(0)
	case "failure":
		os.Exit(7)
	case "sleep":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case "tree":
		child := exec.Command(os.Args[0], "-test.run=^TestExecutorHelperProcess$", "--", "sleep")
		child.Env = append(os.Environ(), "GO_WANT_EXECUTOR_HELPER=1")
		if err := child.Start(); err != nil {
			os.Exit(9)
		}
		fmt.Fprintf(os.Stdout, "child=%d\n", child.Process.Pid)
		time.Sleep(10 * time.Second)
		os.Exit(0)
	default:
		os.Exit(8)
	}
}

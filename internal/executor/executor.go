// Package executor runs argv commands without a shell.
package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Request describes one process execution.
type Request struct {
	Argv             []string
	Dir              string
	Env              []string
	Stdout           io.Writer
	Stderr           io.Writer
	Timeout          time.Duration
	TerminationGrace time.Duration
}

// Result records the terminal state of a process.
type Result struct {
	StartedAt  time.Time
	FinishedAt time.Time
	ExitCode   int
	Signal     string
	TimedOut   bool
	Cancelled  bool
}

// Execute starts request.Argv directly, waits for it, and reports its terminal
// state. A non-zero exit status, signal, timeout, or cancellation is a result,
// not an error. Errors are reserved for failures to set up or start a process.
func Execute(ctx context.Context, request Request) (Result, error) {
	if len(request.Argv) == 0 || request.Argv[0] == "" {
		return Result{}, errors.New("executor: argv must include an executable")
	}

	command := exec.Command(request.Argv[0], request.Argv[1:]...)
	command.Dir = request.Dir
	command.Env = request.Env
	command.Stdout = request.Stdout
	command.Stderr = request.Stderr
	configureCommand(command)

	result := Result{StartedAt: time.Now()}
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("executor: start %q: %w", request.Argv[0], err)
	}

	waitResult := make(chan error, 1)
	go func() {
		waitResult <- command.Wait()
	}()

	var timeout <-chan time.Time
	var stopTimeout func()
	if request.Timeout > 0 {
		timer := time.NewTimer(request.Timeout)
		timeout = timer.C
		stopTimeout = func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
	if stopTimeout != nil {
		defer stopTimeout()
	}

	select {
	case <-waitResult:
		result.FinishedAt = time.Now()
		return finishResult(command, result), nil
	case <-ctx.Done():
		result.Cancelled = true
	case <-timeout:
		result.TimedOut = true
	}

	if !terminate(command, request.TerminationGrace, waitResult) {
		<-waitResult
	}
	result.FinishedAt = time.Now()

	return finishResult(command, result), nil
}

func finishResult(command *exec.Cmd, result Result) Result {
	if state := command.ProcessState; state != nil {
		result.ExitCode = state.ExitCode()
		result.Signal = processSignal(state)
	}

	return result
}

var allowedEnvironment = map[string]struct{}{
	"HOME":           {},
	"PATH":           {},
	"SHELL":          {},
	"USER":           {},
	"LOGNAME":        {},
	"LANG":           {},
	"LANGUAGE":       {},
	"TERM":           {},
	"TMPDIR":         {},
	"TMP":            {},
	"TEMP":           {},
	"TZ":             {},
	"GOCACHE":        {},
	"GOMODCACHE":     {},
	"GOPATH":         {},
	"XDG_CACHE_HOME": {},
}

// FilterEnvironment keeps the supported base environment and applies overrides
// in key order. Overrides replace any base value with the same key.
func FilterEnvironment(base []string, overrides map[string]string) []string {
	filtered := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !allowedEnvironmentKey(key) {
			continue
		}
		if _, overridden := overrides[key]; overridden {
			continue
		}

		filtered = append(filtered, entry)
	}

	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		filtered = append(filtered, key+"="+overrides[key])
	}

	return filtered
}

func allowedEnvironmentKey(key string) bool {
	if strings.HasPrefix(key, "LC_") {
		return true
	}

	_, ok := allowedEnvironment[key]
	return ok
}

//go:build linux || darwin

package executor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminate(command *exec.Cmd, grace time.Duration, waitResult <-chan error) bool {
	if command.Process == nil {
		return false
	}

	pid := command.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = command.Process.Kill()
		return false
	}
	if grace <= 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return false
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-waitResult:
		return true
	case <-timer.C:
	}

	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = command.Process.Kill()
		return false
	}

	return false
}

func processSignal(state *os.ProcessState) string {
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}

	return status.Signal().String()
}

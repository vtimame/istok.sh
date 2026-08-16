//go:build !linux && !darwin

package executor

import (
	"os"
	"os/exec"
	"time"
)

func configureCommand(command *exec.Cmd) {}

func terminate(command *exec.Cmd, _ time.Duration, _ <-chan error) bool {
	if command.Process != nil {
		_ = command.Process.Kill()
	}

	return false
}

func processSignal(_ *os.ProcessState) string { return "" }

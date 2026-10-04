package uiservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnsupported reports a platform without systemd user services.
var ErrUnsupported = errors.New("istok ui service needs Linux with systemd; run `istok ui` directly instead")

func ensureSupported() error {
	if runtime.GOOS != "linux" {
		return ErrUnsupported
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return ErrUnsupported
	}

	return nil
}

// systemctl runs `systemctl --user ...` and includes its output in errors.
func systemctl(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(output))
	}

	return nil
}

// Control runs a lifecycle action (start, stop, restart) on the unit.
func Control(ctx context.Context, action string) error {
	if err := ensureSupported(); err != nil {
		return err
	}

	return systemctl(ctx, action, UnitName)
}

// Status streams `systemctl --user status` to output. An inactive unit is a
// normal answer, not a failure, so its non-zero exit code is not an error.
func Status(ctx context.Context, output io.Writer) error {
	if err := ensureSupported(); err != nil {
		return err
	}

	command := exec.CommandContext(ctx, "systemctl", "--user", "status", "--no-pager", UnitName)
	command.Stdout = output
	command.Stderr = output

	var exitErr *exec.ExitError
	if err := command.Run(); err != nil && !errors.As(err, &exitErr) {
		return fmt.Errorf("systemctl --user status: %w", err)
	}

	return nil
}

package uiservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

// unitName is the systemd user unit that runs the web UI.
const unitName = "istok-ui.service"

// systemd manages the UI as a user unit in $XDG_CONFIG_HOME/systemd/user.
type systemd struct{}

func (systemd) name() string {
	return unitName
}

func (systemd) path() string {
	return filepath.Join(xdg.ConfigHome, "systemd", "user", unitName)
}

func (systemd) notes() []string {
	return []string{"To keep it running after logout: loginctl enable-linger"}
}

func (systemd) render(config Config) string {
	return RenderUnit(config)
}

func (systemd) runs(content []byte, executable string) bool {
	return bytes.Contains(content, []byte("ExecStart="+quoteArgument(executable)+" "))
}

func (systemd) apply(ctx context.Context, changed bool) error {
	if changed {
		if err := systemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
	}

	if err := systemctl(ctx, "enable", unitName); err != nil {
		return err
	}

	return systemctl(ctx, "restart", unitName)
}

func (s systemd) uninstall(ctx context.Context) error {
	if err := systemctl(ctx, "disable", "--now", unitName); err != nil {
		return err
	}
	if err := os.Remove(s.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove unit: %w", err)
	}

	return systemctl(ctx, "daemon-reload")
}

func (systemd) control(ctx context.Context, action string) error {
	return systemctl(ctx, action, unitName)
}

// status streams `systemctl --user status`. An inactive unit exits non-zero,
// which is a normal answer, not an error.
func (systemd) status(ctx context.Context, output io.Writer) error {
	command := exec.CommandContext(ctx, "systemctl", "--user", "status", "--no-pager", unitName)
	command.Stdout = output
	command.Stderr = output

	var exitErr *exec.ExitError
	if err := command.Run(); err != nil && !errors.As(err, &exitErr) {
		return fmt.Errorf("systemctl --user status: %w", err)
	}

	return nil
}

func (systemd) restartIfActive(ctx context.Context) (bool, error) {
	// is-active exits non-zero for inactive or failed units; that only means
	// there is nothing to restart.
	if err := systemctl(ctx, "is-active", "--quiet", unitName); err != nil {
		return false, nil
	}
	if err := systemctl(ctx, "restart", unitName); err != nil {
		return false, err
	}

	return true, nil
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

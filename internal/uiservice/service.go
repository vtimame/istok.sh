// Package uiservice runs `istok ui` as a per-user background service: a
// systemd user unit on Linux and a launchd agent on macOS.
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
	"runtime"
	"strconv"
)

// ErrUnsupported reports a platform without a supported service manager.
var ErrUnsupported = errors.New("istok ui service needs systemd on Linux or launchd on macOS; run `istok ui` directly instead")

// Config describes how the service starts the UI.
type Config struct {
	Executable string
	Port       int
	Database   string
}

// Arguments returns the command line the service runs.
func (c Config) Arguments() []string {
	args := []string{c.Executable, "ui", "--no-open", "--port", strconv.Itoa(c.Port)}
	if c.Database != "" {
		args = append(args, "--database", c.Database)
	}

	return args
}

// manager is one platform's service manager. The shared functions below own
// the service file; a manager renders it and drives the platform tool.
type manager interface {
	name() string
	path() string
	notes() []string
	render(config Config) string

	// runs reports whether the service file starts this exact executable.
	runs(content []byte, executable string) bool

	// apply loads or restarts the service after its file was written.
	apply(ctx context.Context, changed bool) error
	uninstall(ctx context.Context) error
	control(ctx context.Context, action string) error
	status(ctx context.Context, output io.Writer) error

	// restartIfActive restarts a running service and reports whether it did.
	restartIfActive(ctx context.Context) (bool, error)
}

func current() (manager, error) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("systemctl"); err == nil {
			return systemd{}, nil
		}
	case "darwin":
		if _, err := exec.LookPath("launchctl"); err == nil {
			return launchd{uid: os.Getuid()}, nil
		}
	}

	return nil, ErrUnsupported
}

// Name is the service name on this platform, or "" where it is unsupported.
func Name() string {
	m, err := current()
	if err != nil {
		return ""
	}

	return m.name()
}

// Path is the service file on this platform, or "" where it is unsupported.
func Path() string {
	m, err := current()
	if err != nil {
		return ""
	}

	return m.path()
}

// Notes are platform-specific hints printed after an install.
func Notes() []string {
	m, err := current()
	if err != nil {
		return nil
	}

	return m.notes()
}

// Install writes or refreshes the service file and (re)starts the service, so
// a repeated install also picks up a newly built binary.
func Install(ctx context.Context, config Config) (changed bool, err error) {
	m, err := current()
	if err != nil {
		return false, err
	}

	path := m.path()
	content := []byte(m.render(config))

	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read existing service file: %w", err)
	}
	changed = !bytes.Equal(existing, content)

	if changed {
		if err := writeFileAtomic(path, content); err != nil {
			return false, err
		}
	}

	return changed, m.apply(ctx, changed)
}

// Uninstall stops the service and removes its file. A missing file is not an
// error, so it can clean up a partial install.
func Uninstall(ctx context.Context) error {
	m, err := current()
	if err != nil {
		return err
	}

	if _, err := os.Stat(m.path()); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return m.uninstall(ctx)
}

// Control runs a lifecycle action (start, stop, restart) on the service.
func Control(ctx context.Context, action string) error {
	m, err := current()
	if err != nil {
		return err
	}

	return m.control(ctx, action)
}

// Status writes the service manager's view of the service to output. An
// inactive service is a normal answer, not a failure.
func Status(ctx context.Context, output io.Writer) error {
	m, err := current()
	if err != nil {
		return err
	}

	return m.status(ctx, output)
}

// RestartIfActive restarts the service after istok replaced its binary, so
// the UI does not keep running the old process. It only acts when the service
// is installed, runs this exact executable and is active; anything else is
// not an error. It reports whether it restarted the service.
func RestartIfActive(ctx context.Context, executable string) (bool, error) {
	m, err := current()
	if err != nil {
		return false, nil
	}

	content, err := os.ReadFile(m.path())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read service file: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		resolved = executable
	}
	if !m.runs(content, resolved) {
		return false, nil
	}

	return m.restartIfActive(ctx)
}

func writeFileAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create service directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary service file: %w", err)
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write service file: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set service file permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close service file: %w", err)
	}

	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("install service file: %w", err)
	}

	return nil
}

package uiservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// UnitPath is where the user unit lives: $XDG_CONFIG_HOME/systemd/user.
func UnitPath() string {
	return filepath.Join(xdg.ConfigHome, "systemd", "user", UnitName)
}

// Install writes or refreshes the unit, enables it and restarts it, so a
// repeated install also picks up a newly built binary.
func Install(ctx context.Context, config Config) (changed bool, err error) {
	if err := ensureSupported(); err != nil {
		return false, err
	}

	path := UnitPath()
	content := []byte(RenderUnit(config))

	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read existing unit: %w", err)
	}
	changed = !bytes.Equal(existing, content)

	if changed {
		if err := writeFileAtomic(path, content); err != nil {
			return false, err
		}
		if err := systemctl(ctx, "daemon-reload"); err != nil {
			return changed, err
		}
	}

	if err := systemctl(ctx, "enable", UnitName); err != nil {
		return changed, err
	}
	if err := systemctl(ctx, "restart", UnitName); err != nil {
		return changed, err
	}

	return changed, nil
}

// Uninstall stops and disables the unit and removes its file. Missing pieces
// are not errors, so it can clean up a partial install.
func Uninstall(ctx context.Context) error {
	if err := ensureSupported(); err != nil {
		return err
	}

	path := UnitPath()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err := systemctl(ctx, "disable", "--now", UnitName); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove unit: %w", err)
	}

	return systemctl(ctx, "daemon-reload")
}

func writeFileAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create unit directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), "."+UnitName+".*")
	if err != nil {
		return fmt.Errorf("create temporary unit: %w", err)
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write unit: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set unit permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close unit: %w", err)
	}

	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("install unit: %w", err)
	}

	return nil
}

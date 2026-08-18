// Package update orchestrates the applied-update transaction without depending on Fx.
package update

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"

	"github.com/vtimame/istok.sh/internal/storage"
	"github.com/vtimame/istok.sh/internal/updater"
)

const migrationStderrLimit = 64 << 10

type Command struct {
	Check    bool
	Yes      bool
	Database string
}

type Service interface {
	Check(context.Context, string) (*updater.Update, bool, error)
	Apply(context.Context, *selfupdate.Release, string, string) error
	Executable() (string, error)
}

type Runner func(context.Context, string, string) error

type Application struct {
	Service    Service
	Version    string
	Input      io.Reader
	Output     io.Writer
	RunMigrate Runner
}

func (a Application) Run(ctx context.Context, command Command) error {
	update, available, err := a.Service.Check(ctx, a.Version)
	if err != nil {
		return err
	}
	if !available {
		_, err := fmt.Fprintln(a.Output, "Already up to date.")
		return err
	}
	_, _ = fmt.Fprintf(a.Output, "Current: %s\nTarget: %s\nSize: %d bytes\nNotes:\n%s\n", update.Current, update.Target, update.Size, update.Notes)
	if command.Check {
		return nil
	}
	if !command.Yes {
		_, _ = fmt.Fprint(a.Output, "Install update? [y/yes] ")
		answer, readErr := bufio.NewReader(a.Input).ReadString('\n')
		if readErr != nil && len(answer) == 0 {
			return fmt.Errorf("read update confirmation: %w", readErr)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			_, err := fmt.Fprintln(a.Output, "Update cancelled.")
			return err
		}
	}

	database := command.Database
	if database == "" {
		database = storage.DefaultPath()
	}
	if database == ":memory:" {
		return errors.New("in-memory database cannot be used for an applied update")
	}
	backup, existed, err := storage.Backup(ctx, database)
	if err != nil {
		return err
	}
	target, err := a.Service.Executable()
	if err != nil {
		return cleanupBackupError(fmt.Errorf("locate executable: %w", err), backup)
	}
	old, err := uniquePath(target, ".istok-update-old-*")
	if err != nil {
		return cleanupBackupError(err, backup)
	}
	if err := a.Service.Apply(ctx, update.Release, target, old); err != nil {
		return cleanupFailedApply(fmt.Errorf("apply update: %w", err), target, backup, old)
	}

	runMigrate := a.RunMigrate
	if runMigrate == nil {
		runMigrate = ApplyMigrations
	}
	if err := runMigrate(ctx, target, database); err != nil {
		recoveryErr := Recover(target, old, database, backup, existed)
		if recoveryErr != nil {
			return fmt.Errorf("updated binary migration failed: %w; automatic recovery failed: %v; recovery artifacts: old binary %q, database backup %q", err, recoveryErr, old, backup)
		}
		return fmt.Errorf("updated binary migration failed and update was recovered: %w", err)
	}
	if err := cleanupSuccessfulUpdate(target, old, backup); err != nil {
		return fmt.Errorf("updated successfully, but cleanup failed: %w", err)
	}
	_, err = fmt.Fprintf(a.Output, "Updated successfully to %s.\n", update.Target)
	return err
}

func cleanupBackupError(original error, backup string) error {
	if backup == "" {
		return original
	}
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w; retain database backup %q because cleanup failed: %v", original, backup, err)
	}
	return original
}

func cleanupFailedApply(original error, target, backup, old string) error {
	if old == "" {
		return cleanupBackupError(original, backup)
	}
	if _, err := os.Lstat(old); err == nil {
		if recoveryErr := RestoreBinary(target, old); recoveryErr != nil {
			original = fmt.Errorf("%w; updater retained binary artifact %q and automatic executable recovery failed: %v", original, old, recoveryErr)
		} else {
			original = fmt.Errorf("%w; updater executable state was recovered", original)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		original = fmt.Errorf("%w; inspect updater artifact %q: %v", original, old, err)
	}

	return cleanupBackupError(original, backup)
}

func cleanupSuccessfulUpdate(target, old, backup string) error {
	if runtime.GOOS == "windows" && old != "" {
		return scheduleWindowsCleanup(target, old, backup)
	}

	var result error
	for _, path := range []string{old, backup} {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove update artifact %q: %w", path, err))
		}
	}
	return result
}

func scheduleWindowsCleanup(target, old, backup string) error {
	args := []string{"cleanup", "--pid", fmt.Sprint(os.Getpid()), "--path", old}
	if backup != "" {
		args = append(args, "--path", backup)
	}
	command := exec.Command(target, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return fmt.Errorf("start deferred Windows cleanup for artifacts %q and %q: %w", old, backup, err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("release deferred Windows cleanup process: %w", err)
	}
	return nil
}

// CleanupAfterParentExit waits for the updating process before removing artifacts.
// It is run by the newly installed binary so Windows no longer has the old binary open.
func CleanupAfterParentExit(pid int, paths []string) error {
	if pid <= 0 {
		return fmt.Errorf("invalid parent process ID %d", pid)
	}
	parent, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find updating process %d: %w", pid, err)
	}
	_, _ = parent.Wait()
	_ = parent.Release()

	var result error
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove deferred update artifact %q: %w", path, err))
		}
	}
	return result
}

func uniquePath(target, pattern string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(target), pattern)
	if err != nil {
		return "", fmt.Errorf("create update artifact path: %w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close update artifact path: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("prepare update artifact path: %w", err)
	}
	return name, nil
}

func ApplyMigrations(ctx context.Context, executable, database string) error {
	childCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(childCtx, executable, "migrate", "--database", database)
	command.Stdout = io.Discard
	var stderr limitedBuffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if text := sanitizeStderr(stderr.String()); text != "" {
			return fmt.Errorf("run updated binary migrations: %w: %s", err, text)
		}
		return fmt.Errorf("run updated binary migrations: %w", err)
	}
	return nil
}

type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := migrationStderrLimit - len(b.data); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *limitedBuffer) String() string { return string(b.data) }

func sanitizeStderr(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.TrimSpace(value)
	if len(value) > migrationStderrLimit {
		value = value[:migrationStderrLimit]
	}
	return value
}

func Recover(target, old, database, backup string, existed bool) error {
	var result error
	if backup != "" || !existed {
		result = errors.Join(result, storage.Restore(database, backup, existed))
	}
	if old != "" {
		result = errors.Join(result, RestoreBinary(target, old))
	}
	return result
}

// RestoreBinary promotes the saved executable without relying on rename-overwrite,
// which is unsupported while the target exists on Windows.
func RestoreBinary(target, old string) error {
	failed := ""
	if _, err := os.Lstat(target); err == nil {
		failed, err = uniquePath(target, ".istok-update-new-failed-*")
		if err != nil {
			return err
		}
		if err := os.Rename(target, failed); err != nil {
			return fmt.Errorf("stage updated binary %q for rollback: %w", target, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect updated binary %q for rollback: %w", target, err)
	}
	if err := os.Rename(old, target); err != nil {
		recoveryErr := restoreStagedBinary(failed, target)
		return fmt.Errorf("restore previous binary %q to %q: %w; restore updated binary from %q: %v; retained artifacts: old binary %q, failed update %q", old, target, err, failed, recoveryErr, old, failed)
	}
	if failed != "" {
		if err := os.Remove(failed); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rolled back binary but remove failed update artifact %q: %w", failed, err)
		}
	}

	return nil
}

func restoreStagedBinary(stagedPath, targetPath string) error {
	if stagedPath == "" {
		return nil
	}

	return os.Rename(stagedPath, targetPath)
}

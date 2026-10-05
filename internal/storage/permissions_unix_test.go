//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"go.uber.org/fx"
)

// withUmask sets the process umask for one test. The umask is process-wide, so
// tests that use it must not run in parallel.
func withUmask(t *testing.T, mask int) {
	t.Helper()

	previous := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(previous) })
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %04o, want %04o", path, got, want)
	}
}

func TestPrepareDatabasePathCreatesPrivateDatabaseUnderPermissiveUmask(t *testing.T) {
	withUmask(t, 0o022)

	directory := filepath.Join(t.TempDir(), "custom", "nested")
	path := filepath.Join(directory, "istok.db")

	if err := prepareDatabasePath(path, false); err != nil {
		t.Fatalf("prepareDatabasePath() error = %v", err)
	}

	assertMode(t, path, 0o600)
	assertMode(t, directory, 0o700)
}

func TestPrepareDatabasePathFixesExistingWideDatabase(t *testing.T) {
	withUmask(t, 0o022)

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatalf("chmod directory: %v", err)
	}

	path := filepath.Join(directory, "istok.db")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing database: %v", err)
	}
	assertMode(t, path, 0o644)

	if err := prepareDatabasePath(path, false); err != nil {
		t.Fatalf("prepareDatabasePath() error = %v", err)
	}

	assertMode(t, path, 0o600)

	// A custom parent that is safe already keeps its mode.
	assertMode(t, directory, 0o755)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read database: %v", err)
	}
	if string(content) != "existing" {
		t.Errorf("database content = %q, want it untouched", content)
	}
}

func TestPrepareDatabasePathForcesDefaultDirectoryPrivate(t *testing.T) {
	withUmask(t, 0o022)

	directory := filepath.Join(t.TempDir(), "istok")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	path := filepath.Join(directory, "istok.db")
	if err := prepareDatabasePath(path, true); err != nil {
		t.Fatalf("prepareDatabasePath() error = %v", err)
	}

	assertMode(t, directory, 0o700)
	assertMode(t, path, 0o600)
}

func TestPrepareDatabasePathRejectsCustomParentWritableByOthers(t *testing.T) {
	for _, mode := range []os.FileMode{0o775, 0o757, 0o777} {
		directory := filepath.Join(t.TempDir(), "shared")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create directory: %v", err)
		}
		if err := os.Chmod(directory, mode); err != nil {
			t.Fatalf("chmod directory: %v", err)
		}

		path := filepath.Join(directory, "istok.db")
		err := prepareDatabasePath(path, false)
		if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
			t.Errorf("mode %04o: prepareDatabasePath() error = %v, want writable-parent rejection", mode, err)
		}

		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Errorf("mode %04o: database was created in an unsafe directory", mode)
		}
	}
}

func TestPrepareDatabasePathRejectsSymlinkedDatabase(t *testing.T) {
	directory := t.TempDir()

	target := filepath.Join(directory, "target.db")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}

	path := filepath.Join(directory, "istok.db")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := prepareDatabasePath(path, false); err == nil {
		t.Fatal("prepareDatabasePath() accepted a symlinked database")
	}

	// The symlink target must not be touched.
	assertMode(t, target, 0o644)
}

func TestPrepareDatabasePathRejectsNonRegularFile(t *testing.T) {
	directory := t.TempDir()

	path := filepath.Join(directory, "istok.db")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo not available: %v", err)
	}

	err := prepareDatabasePath(path, false)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("prepareDatabasePath() error = %v, want non-regular file rejection", err)
	}
}

func TestStorageOpensCustomDatabaseWithPrivateMode(t *testing.T) {
	withUmask(t, 0o022)

	path := filepath.Join(t.TempDir(), "data", "istok.db")

	var db *sql.DB
	app := fx.New(
		fx.NopLogger,
		Module(Config{Path: path}),
		fx.Invoke(func(value *sql.DB) { db = value }),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	// Write through SQLite so the file mode is checked after real use.
	if _, err := db.ExecContext(ctx, "CREATE TABLE permissions_probe (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("write through SQLite: %v", err)
	}

	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)
}

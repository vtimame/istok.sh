package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"go.uber.org/fx"
)

func TestDefaultPathUsesXDGDataHome(t *testing.T) {
	dataHome := t.TempDir()
	previous, wasSet := os.LookupEnv("XDG_DATA_HOME")
	if err := os.Setenv("XDG_DATA_HOME", dataHome); err != nil {
		t.Fatalf("set XDG_DATA_HOME: %v", err)
	}
	xdg.Reload()
	t.Cleanup(func() {
		if wasSet {
			if err := os.Setenv("XDG_DATA_HOME", previous); err != nil {
				t.Errorf("restore XDG_DATA_HOME: %v", err)
			}
		} else if err := os.Unsetenv("XDG_DATA_HOME"); err != nil {
			t.Errorf("unset XDG_DATA_HOME: %v", err)
		}

		xdg.Reload()
	})

	if got, want := DefaultPath(), filepath.Join(xdg.DataHome, "istok", "istok.db"); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestEmbeddedMigrationIsApplied(t *testing.T) {
	var db *sql.DB
	app := fx.New(
		fx.NopLogger,
		Module(Config{Path: filepath.Join(t.TempDir(), "data", "istok.db")}),
		fx.Invoke(func(value *sql.DB) { db = value }),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := app.Stop(ctx); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	}()

	var version int
	if err := db.QueryRowContext(ctx, "SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("query goose version: %v", err)
	}

	if version != 4 {
		t.Errorf("migration version = %d, want 4", version)
	}
}

func TestSQLiteDSNEscapesSpecialPathAndOpensDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "name #?.db")
	parsed, err := url.Parse(sqliteDSN(path))
	if err != nil {
		t.Fatalf("parse SQLite DSN: %v", err)
	}
	if parsed.Path != path {
		t.Fatalf("DSN path = %q, want %q", parsed.Path, path)
	}
	if got := parsed.Query().Get("_foreign_keys"); got != "on" {
		t.Errorf("foreign keys option = %q", got)
	}
	if got := parsed.Query().Get("_busy_timeout"); got != "5000" {
		t.Errorf("busy timeout option = %q", got)
	}

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
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat database path: %v", err)
	}
}

func TestSQLiteDSNUsesMemoryURI(t *testing.T) {
	if got := sqliteDSN(":memory:"); !strings.HasPrefix(got, "file::memory:") {
		t.Errorf("memory DSN = %q", got)
	}
}

func TestBackupAndRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "name #?.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE values_test (value TEXT); INSERT INTO values_test VALUES ('before')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	backup, existed, err := Backup(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Fatal("expected existing database")
	}
	if mode, err := os.Stat(backup); err != nil || mode.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, err = %v", mode, err)
	}

	db, err = sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM values_test"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Restore(path, backup, true); err != nil {
		t.Fatal(err)
	}

	db, err = sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM values_test").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "before" {
		t.Fatalf("restored value = %q", value)
	}
}

func TestRestoreRemovesStaleSidecars(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "data.db")
	if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(directory, "backup.db")
	if err := os.WriteFile(backup, []byte("backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(path+suffix, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Restore(path, backup, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "backup" {
		t.Fatalf("restored data = %q, err = %v", got, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("sidecar %s remains: %v", suffix, err)
		}
	}
}

func TestRestoreAbsentDatabaseRemovesNewDatabaseAndSidecars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(path+suffix, []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Restore(path, "", false); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("database artifact %s remains: %v", suffix, err)
		}
	}
}

func TestRestoreExistingDatabaseWhenCurrentFileWasRemoved(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "data.db")
	backup := filepath.Join(directory, "backup.db")
	if err := os.WriteFile(backup, []byte("backup"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Restore(path, backup, true); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "backup" {
		t.Fatalf("restored data = %q, err = %v", got, err)
	}
}

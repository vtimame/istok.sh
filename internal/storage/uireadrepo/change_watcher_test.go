package uireadrepo

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openSingleConnection(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	// Mirror storage.New: one connection per pool.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestChangeWatcherReportsCommitsFromOtherConnections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	watched := openSingleConnection(t, path)
	other := openSingleConnection(t, path)

	if _, err := other.ExecContext(ctx, "CREATE TABLE items (value INTEGER)"); err != nil {
		t.Fatal(err)
	}

	watcher, err := New(watched).NewChangeWatcher(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if changed, err := watcher.Changed(ctx); err != nil || changed {
		t.Fatalf("Changed() before any commit = %v, %v; want false", changed, err)
	}

	if _, err := other.ExecContext(ctx, "INSERT INTO items VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if changed, err := watcher.Changed(ctx); err != nil || !changed {
		t.Fatalf("Changed() after another connection committed = %v, %v; want true", changed, err)
	}
	if changed, err := watcher.Changed(ctx); err != nil || changed {
		t.Fatalf("Changed() with no new commit = %v, %v; want false", changed, err)
	}

	// The watcher's own connection does not move data_version; writers in the
	// same process signal those changes themselves.
	if _, err := watched.ExecContext(ctx, "INSERT INTO items VALUES (2)"); err != nil {
		t.Fatal(err)
	}
	if changed, err := watcher.Changed(ctx); err != nil || changed {
		t.Fatalf("Changed() after a commit on the watched connection = %v, %v; want false", changed, err)
	}
}

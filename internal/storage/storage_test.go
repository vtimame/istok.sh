package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/pressly/goose/v3"
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

func TestRunWorkflowMigrationBackfillsExistingRunLease(t *testing.T) {
	database := filepath.Join(t.TempDir(), "backfill.db")
	db, err := sql.Open("sqlite3", sqliteDSN(database))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(context.Background(), 6); err != nil {
		t.Fatal(err)
	}

	stamp := "2026-08-16T12:00:00Z"
	statements := []string{
		`INSERT INTO projects (id,name,revision,created_at,updated_at) VALUES ('project','Project',1,?,?)`,
		`INSERT INTO tasks (id,project_id,number,revision,status,title,created_at,updated_at) VALUES ('task','project',1,1,'open','Task',?,?)`,
		`INSERT INTO context_snapshots (id,schema_version,project_id,generated_at,created_at) VALUES ('snapshot','1','project',?,?)`,
		`INSERT INTO runs (id,task_id,context_snapshot_id,revision,status,actor_id,actor_kind,actor_name,started_at,updated_at) VALUES ('run','task','snapshot',1,'active','actor','cli','CLI',?,?)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement, stamp, stamp); err != nil {
			t.Fatalf("seed v6 state: %v", err)
		}
	}

	if _, err := provider.UpTo(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var leaseID, heartbeatAt, expiresAt string
	if err := db.QueryRow(`SELECT lease_id,heartbeat_at,expires_at FROM run_leases WHERE run_id='run'`).Scan(&leaseID, &heartbeatAt, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if leaseID != "run" || heartbeatAt != stamp || expiresAt != stamp {
		t.Fatalf("backfilled lease = %q, %q, %q", leaseID, heartbeatAt, expiresAt)
	}
}

func TestInstructionPolicyMigrationBackfillsWithoutChangingIdentityOrHistory(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "instruction-policy.db")
	db, err := sql.Open("sqlite3", sqliteDSN(database))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}

	stamp := "2026-08-17T10:00:00Z"
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id,name,revision,created_at,updated_at) VALUES ('project','Project',1,?,?)`, []any{stamp, stamp}},
		{`INSERT INTO context_records (id,project_id,revision,kind,title,body,tags,source,visibility,sensitivity,actor_id,actor_kind,actor_name,created_at,updated_at) VALUES ('instruction','project',1,'instruction','Policy','Body','[]','user','shared','normal','actor','user','User',?,?)`, []any{stamp, stamp}},
		{`INSERT INTO context_events (id,record_id,type,body,record_revision,actor_id,actor_kind,actor_name,created_at) VALUES ('event','instruction','created','',1,'actor','user','User',?)`, []any{stamp}},
		{`INSERT INTO context_snapshots (id,schema_version,project_id,generated_at,created_at) VALUES ('snapshot','2','project',?,?)`, []any{stamp, stamp}},
		{`INSERT INTO context_snapshot_items (snapshot_id,position,record_id,record_revision,content_hash,kind,source,visibility,sensitivity,title,body,snippet,tags) VALUES ('snapshot',0,'instruction',1,'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff','instruction','user','shared','normal','Policy','Body','Body','[]')`, nil},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed v8 state: %v", err)
		}
	}

	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	var enabled int
	var priority, scope string
	if err := db.QueryRowContext(ctx, `SELECT enabled,priority,scope FROM context_records WHERE id='instruction'`).Scan(&enabled, &priority, &scope); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || priority != "normal" || scope != "project" {
		t.Fatalf("record policy = %d, %q, %q", enabled, priority, scope)
	}
	if err := db.QueryRowContext(ctx, `SELECT enabled,priority,scope FROM context_snapshot_items WHERE snapshot_id='snapshot'`).Scan(&enabled, &priority, &scope); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || priority != "normal" || scope != "project" {
		t.Fatalf("snapshot policy = %d, %q, %q", enabled, priority, scope)
	}
	var eventCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM context_events WHERE id='event' AND record_id='instruction'`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("event history count = %d, err = %v", eventCount, err)
	}

	if _, err := provider.DownTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	var policyColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('context_records') WHERE name IN ('enabled','priority','scope')`).Scan(&policyColumns); err != nil {
		t.Fatal(err)
	}
	if policyColumns != 0 {
		t.Fatalf("policy columns after down = %d", policyColumns)
	}
}

func TestRunAbandonedMigrationRoundTripPreservesChildrenAndIndexes(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "abandoned.db")
	db, err := sql.Open("sqlite3", sqliteDSN(database))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}

	stamp := "2026-08-17T10:00:00Z"
	queries := []string{
		`INSERT INTO projects (id,name,revision,created_at,updated_at) VALUES ('project','Project',1,'2026-08-17T10:00:00Z','2026-08-17T10:00:00Z')`,
		`INSERT INTO tasks (id,project_id,number,revision,status,title,created_at,updated_at) VALUES ('task','project',1,1,'open','Task','2026-08-17T10:00:00Z','2026-08-17T10:00:00Z')`,
		`INSERT INTO context_snapshots (id,schema_version,project_id,generated_at,created_at) VALUES ('snapshot','2','project','2026-08-17T10:00:00Z','2026-08-17T10:00:00Z')`,
		`INSERT INTO runs (id,task_id,context_snapshot_id,revision,status,actor_id,actor_kind,actor_name,started_at,updated_at) VALUES ('run','task','snapshot',1,'active','actor','agent','Agent','2026-08-17T10:00:00Z','2026-08-17T10:00:00Z')`,
		`INSERT INTO run_leases (run_id,lease_id,owner_id,owner_kind,owner_name,heartbeat_at,expires_at,updated_at) VALUES ('run','lease','actor','agent','Agent','2026-08-17T10:00:00Z','2026-08-17T10:00:00Z','2026-08-17T10:00:00Z')`,
		`INSERT INTO executions (id,run_id,revision,status,argv,cwd,actor_id,actor_kind,actor_name,started_at,updated_at,finished_at) VALUES ('execution','run',1,'cancelled','["true"]','.', 'actor','agent','Agent','2026-08-17T10:00:00Z','2026-08-17T10:00:00Z','2026-08-17T10:00:00Z')`,
		`INSERT INTO validations (id,execution_id,source,status,command,summary,actor_id,actor_kind,actor_name,created_at) VALUES ('validation','execution','attested','passed','true','passed','actor','agent','Agent','2026-08-17T10:00:00Z')`,
		`INSERT INTO artifacts (id,run_id,execution_id,kind,relative_path,sha256,original_size,stored_size,truncated,media_type,actor_id,actor_kind,actor_name,created_at) VALUES ('artifact','run','execution','stdout','stdout.log','ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',0,0,0,'text/plain','actor','agent','Agent','2026-08-17T10:00:00Z')`,
		`INSERT INTO task_completions (id,task_id,run_id,validation_id,note,actor_id,actor_kind,actor_name,created_at) VALUES ('completion','task','run','validation','done','actor','agent','Agent','2026-08-17T10:00:00Z')`,
	}
	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("seed v9 state: %v", err)
		}
	}

	if _, err := provider.UpTo(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE runs SET status='abandoned',finished_at=?,finished_actor_id='actor',finished_actor_kind='agent',finished_actor_name='Agent' WHERE id='run'`, stamp); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"run_leases", "executions", "validations", "artifacts", "task_completions"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var indexCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_index_list('runs') WHERE name IN ('runs_one_active_per_task','runs_task_started')`).Scan(&indexCount); err != nil || indexCount != 2 {
		t.Fatalf("run indexes=%d err=%v", indexCount, err)
	}
	assertRunChildForeignKeys(t, db)
	var violations int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations after up=%d err=%v", violations, err)
	}

	if _, err := provider.DownTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM runs WHERE id='run'`).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("down status=%q err=%v", status, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", violations, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_index_list('runs') WHERE name IN ('runs_one_active_per_task','runs_task_started')`).Scan(&indexCount); err != nil || indexCount != 2 {
		t.Fatalf("run indexes after down=%d err=%v", indexCount, err)
	}
	assertRunChildForeignKeys(t, db)
}

func assertRunChildForeignKeys(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, table := range []string{"run_leases", "executions", "artifacts", "task_completions"} {
		var count int
		query := fmt.Sprintf(`SELECT COUNT(*) FROM pragma_foreign_key_list('%s') WHERE "table"='runs'`, table)
		if err := db.QueryRow(query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s foreign keys to runs=%d err=%v", table, count, err)
		}
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

	if version != 10 {
		t.Errorf("migration version = %d, want 10", version)
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

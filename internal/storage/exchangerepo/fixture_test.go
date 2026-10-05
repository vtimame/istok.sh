package exchangerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/exchange"
	"github.com/vtimame/istok.sh/internal/storage"
)

var testActor = Actor{ID: "test", Kind: "cli", Name: "Test"}

// device is one migrated database standing in for one machine.
type device struct {
	t    *testing.T
	db   *sql.DB
	repo *Repository
}

func newDevice(t *testing.T) *device {
	t.Helper()

	var db *sql.DB
	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: filepath.Join(t.TempDir(), "istok.db")}),
		fx.Invoke(func(value *sql.DB) { db = value }),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start migrated SQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Errorf("stop SQLite: %v", err)
		}
	})

	return &device{t: t, db: db, repo: New(db)}
}

func (d *device) exec(query string, args ...any) {
	d.t.Helper()

	if _, err := d.db.Exec(query, args...); err != nil {
		d.t.Fatalf("exec %q: %v", query, err)
	}
}

func (d *device) count(query string, args ...any) int {
	d.t.Helper()

	var value int
	if err := d.db.QueryRow(query, args...).Scan(&value); err != nil {
		d.t.Fatalf("count %q: %v", query, err)
	}

	return value
}

func (d *device) text(query string, args ...any) string {
	d.t.Helper()

	var value string
	if err := d.db.QueryRow(query, args...).Scan(&value); err != nil {
		d.t.Fatalf("read %q: %v", query, err)
	}

	return value
}

func (d *device) export(projectIDs ...string) exchange.Bundle {
	d.t.Helper()

	bundle, err := d.repo.Export(context.Background(), projectIDs)
	if err != nil {
		d.t.Fatalf("Export() error = %v", err)
	}

	// Go through JSON like a real file, so number handling is exercised.
	encoded, err := json.Marshal(bundle)
	if err != nil {
		d.t.Fatalf("marshal bundle: %v", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()

	var decoded exchange.Bundle
	if err := decoder.Decode(&decoded); err != nil {
		d.t.Fatalf("decode bundle: %v", err)
	}

	return decoded
}

func (d *device) importBundle(bundle exchange.Bundle, dryRun bool) exchange.Report {
	d.t.Helper()

	report, err := d.repo.Import(context.Background(), bundle, testActor, dryRun)
	if err != nil {
		d.t.Fatalf("Import() error = %v", err)
	}

	return report
}

// Fixed IDs keep the fixture readable. They only need to be unique.
const (
	projectID     = "01a10000-0000-7000-8000-000000000001"
	taskOneID     = "01a10000-0000-7000-8000-000000000011"
	taskTwoID     = "01a10000-0000-7000-8000-000000000012"
	dependencyID  = "01a10000-0000-7000-8000-000000000021"
	sharedNoteID  = "01a10000-0000-7000-8000-000000000031"
	localNoteID   = "01a10000-0000-7000-8000-000000000032"
	sharedItemID  = "01a10000-0000-7000-8000-000000000041"
	localItemID   = "01a10000-0000-7000-8000-000000000042"
	snapshotID    = "01a10000-0000-7000-8000-000000000051"
	runID         = "01a10000-0000-7000-8000-000000000061"
	executionID   = "01a10000-0000-7000-8000-000000000071"
	artifactID    = "01a10000-0000-7000-8000-000000000081"
	validationID  = "01a10000-0000-7000-8000-000000000091"
	completionID  = "01a10000-0000-7000-8000-0000000000a1"
	actorColumns  = "actor_id, actor_kind, actor_name"
	actorValues   = "'agent-1', 'agent', 'Agent'"
	sampleHash    = "0000000000000000000000000000000000000000000000000000000000000000"
	createdOffset = time.Hour
)

func stampAt(offset time.Duration) string {
	return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Add(offset).Format(time.RFC3339Nano)
}

// seedProject fills a device with one project that uses every exported table,
// plus local-only data that must never leave it.
func (d *device) seedProject(name string) {
	d.t.Helper()

	created := stampAt(0)
	d.exec("INSERT INTO projects (id, name, revision, created_at, updated_at) VALUES (?, ?, 1, ?, ?)", projectID, name, created, created)
	d.exec("INSERT INTO project_roots (project_id, canonical_path, path_key, active_at) VALUES (?, '/home/me/acme', '/home/me/acme', ?)", projectID, created)
	d.exec("INSERT INTO project_task_counters (project_id, next_number) VALUES (?, 3)", projectID)

	d.addTask(taskOneID, 1, "Add pagination", stampAt(createdOffset))
	d.addTask(taskTwoID, 2, "Show the page size", stampAt(2*createdOffset))
	d.exec("INSERT INTO task_dependencies (id, project_id, blocker_task_id, blocked_task_id, created_at) VALUES (?, ?, ?, ?, ?)",
		dependencyID, projectID, taskOneID, taskTwoID, created)

	for _, record := range []struct{ id, visibility string }{{sharedNoteID, "shared"}, {localNoteID, "local_only"}} {
		d.exec("INSERT INTO context_records (id, project_id, kind, title, body, source, visibility, sensitivity, "+actorColumns+", created_at, updated_at) "+
			"VALUES (?, ?, 'note', 'Note', 'Body', 'agent', ?, 'normal', "+actorValues+", ?, ?)", record.id, projectID, record.visibility, created, created)
		d.exec("INSERT INTO context_events (id, record_id, type, record_revision, "+actorColumns+", created_at) VALUES (?, ?, 'created', 1, "+actorValues+", ?)",
			record.id+"-e", record.id, created)
	}

	for _, item := range []struct{ id, visibility string }{{sharedItemID, "shared"}, {localItemID, "local_only"}} {
		d.exec("INSERT INTO knowledge_items (id, project_id, kind, title, summary, visibility, sensitivity, content_hash, "+actorColumns+", created_at, updated_at) "+
			"VALUES (?, ?, 'note', 'Item', 'Summary', ?, 'normal', ?, "+actorValues+", ?, ?)", item.id, projectID, item.visibility, sampleHash, created, created)
	}

	d.exec("INSERT INTO context_snapshots (id, schema_version, project_id, generated_at, created_at) VALUES (?, '5', ?, ?, ?)", snapshotID, projectID, created, created)
	for position, record := range []struct{ id, visibility string }{{sharedNoteID, "shared"}, {localNoteID, "local_only"}} {
		d.exec("INSERT INTO context_snapshot_items (snapshot_id, position, record_id, record_revision, content_hash, kind, source, visibility, sensitivity, title) "+
			"VALUES (?, ?, ?, 1, ?, 'note', 'agent', ?, 'normal', 'Note')", snapshotID, position, record.id, sampleHash, record.visibility)
	}
	d.exec("INSERT INTO context_snapshot_retrieval_items (snapshot_id, position, item_id, contract_version, chunk_id, kind, path, language, line_start, line_end, "+
		"content_hash, snippet, score, lexical_score, graph_score, visibility) VALUES (?, 0, 'item', 'v1', 'chunk', 'file_chunk', 'main.go', 'go', 1, 2, ?, "+
		"'package main', 1, 1, 0, 'local_only')", snapshotID, sampleHash)

	d.exec("INSERT INTO runs (id, task_id, context_snapshot_id, status, "+actorColumns+", started_at, updated_at, finished_at, "+
		"finished_actor_id, finished_actor_kind, finished_actor_name, result_summary) VALUES (?, ?, ?, 'succeeded', "+actorValues+", ?, ?, ?, 'agent-1', 'agent', 'Agent', 'Done')",
		runID, taskOneID, snapshotID, created, created, created)
	d.exec("INSERT INTO run_leases (run_id, lease_id, owner_id, owner_kind, owner_name, heartbeat_at, expires_at, updated_at) VALUES (?, 'lease-1', 'agent-1', 'agent', 'Agent', ?, ?, ?)",
		runID, created, created, created)
	d.exec("INSERT INTO executions (id, run_id, status, argv, cwd, exit_code, duration_ms, "+actorColumns+", started_at, updated_at, finished_at) "+
		"VALUES (?, ?, 'succeeded', '[\"go\",\"test\"]', '/home/me/acme', 0, 10, "+actorValues+", ?, ?, ?)", executionID, runID, created, created, created)
	d.exec("INSERT INTO artifacts (id, run_id, execution_id, kind, relative_path, sha256, original_size, stored_size, truncated, media_type, "+actorColumns+", created_at) "+
		"VALUES (?, ?, ?, 'stdout', 'runs/x/stdout.log', ?, 0, 0, 0, 'text/plain', "+actorValues+", ?)", artifactID, runID, executionID, sampleHash, created)
	d.exec("INSERT INTO validations (id, execution_id, source, status, command, exit_code, summary, "+actorColumns+", created_at) "+
		"VALUES (?, ?, 'executed', 'passed', 'go test', 0, 'passed', "+actorValues+", ?)", validationID, executionID, created)
	d.exec("INSERT INTO task_completions (id, task_id, run_id, validation_id, note, "+actorColumns+", created_at) VALUES (?, ?, ?, ?, 'Done', "+actorValues+", ?)",
		completionID, taskOneID, runID, validationID, created)
	d.exec("UPDATE tasks SET status = 'done' WHERE id = ?", taskOneID)
}

func (d *device) addTask(id string, number int, title, created string) {
	d.t.Helper()

	d.exec("INSERT INTO tasks (id, project_id, number, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", id, projectID, number, title, created, created)
	d.exec("INSERT INTO task_events (id, task_id, type, task_revision, "+actorColumns+", created_at) VALUES (?, ?, 'created', 1, "+actorValues+", ?)",
		id+"-e", id, created)
}

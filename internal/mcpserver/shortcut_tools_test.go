package mcpserver

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	runmodel "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

func TestMCPShortWorkflowSurvivesAnExpiredLease(t *testing.T) {
	database := filepath.Join(t.TempDir(), "short.db")
	root := t.TempDir()
	_, session := newSessionAtDatabaseWithActor(t, Worker, root, database, "short-agent")
	initProject(t, session, "short workflow")

	created := callTaskResult(t, session, "task_create", map[string]any{"title": "server generates the id"})
	if !task.IsUUIDv7(created.Task.ID) {
		t.Fatalf("generated task id = %q", created.Task.ID)
	}

	for _, body := range []string{"first note", "second note"} {
		callTaskResult(t, session, "task_progress", map[string]any{"task_id": created.Task.ID, "body": body})
	}

	claimed := callTool(t, session, "task_claim", map[string]any{"task_id": created.Task.ID})
	var claim RunResult
	decodeStructured(t, claimed, &claim)
	if claim.Run == nil {
		t.Fatalf("claim = %+v", claim)
	}

	expireLease(t, database, claim.Run.ID)

	callTaskResult(t, session, "task_progress", map[string]any{"task_id": created.Task.ID, "body": "back after a pause"})
	resumed := showRun(t, session, claim.Run.ID)
	if resumed.Run.LeaseID != claim.Run.LeaseID || !resumed.Run.ExpiresAt.After(time.Now()) {
		t.Fatalf("progress did not resume the lease: %+v", resumed.Run)
	}

	expireLease(t, database, claim.Run.ID)

	recorded := callTool(t, session, "task_evidence", map[string]any{"task_id": created.Task.ID, "status": "passed", "summary": "manual e2e on the stand"})
	var evidence EvidenceResult
	decodeStructured(t, recorded, &evidence)
	if recorded.IsError || evidence.Validation == nil || evidence.Validation.Source != runmodel.ValidationSourceAttested {
		t.Fatalf("task_evidence = %+v", evidence)
	}

	closed := callTool(t, session, "task_done", map[string]any{"task_id": created.Task.ID, "summary": "login fixed"})
	var done TaskDoneResult
	decodeStructured(t, closed, &done)
	if closed.IsError || done.Task == nil || done.Task.Status != task.StatusDone {
		t.Fatalf("task_done = %+v", done)
	}
	if done.Run.Status != runmodel.StatusSucceeded || done.Completion.ValidationID == nil || *done.Completion.ValidationID != evidence.Validation.ID {
		t.Fatalf("task_done run %+v completion %+v", done.Run, done.Completion)
	}
}

func TestMCPTaskDoneNeedsEvidenceOrAReason(t *testing.T) {
	root := t.TempDir()
	_, session := newSessionAtDatabaseWithActor(t, Worker, root, filepath.Join(t.TempDir(), "done.db"), "done-agent")
	initProject(t, session, "done")

	created := callTaskResult(t, session, "task_create", map[string]any{"title": "no tests allowed"})

	unclaimed := callTool(t, session, "task_done", map[string]any{"task_id": created.Task.ID, "summary": "x", "not_verified_reason": "y"})
	if !unclaimed.IsError {
		t.Fatalf("task_done without a run succeeded: %+v", unclaimed)
	}

	callTool(t, session, "task_claim", map[string]any{"task_id": created.Task.ID})

	missing := callTool(t, session, "task_done", map[string]any{"task_id": created.Task.ID, "summary": "implemented"})
	assertRunToolCode(t, missing, runmodel.CodeEvidenceRequired)

	closed := callTool(t, session, "task_done", map[string]any{"task_id": created.Task.ID, "summary": "implemented", "not_verified_reason": "the user did not allow running the project"})
	var done TaskDoneResult
	decodeStructured(t, closed, &done)
	if closed.IsError || done.Task == nil || done.Task.Status != task.StatusDone {
		t.Fatalf("unverified task_done = %+v", done)
	}
	if done.Completion.OverrideReason != "the user did not allow running the project" || done.Run.ValidationOverride == "" {
		t.Fatalf("unverified completion %+v run %+v", done.Completion, done.Run)
	}
}

func TestMCPTaskNotesStillHonorAnExpectedRevision(t *testing.T) {
	_, session := newSession(t, Worker, t.TempDir())
	initProject(t, session, "cas")
	created := callTaskResult(t, session, "task_create", map[string]any{"title": "cas"})

	stale := callTool(t, session, "task_comment", map[string]any{"task_id": created.Task.ID, "expected_revision": created.Task.Revision + 5, "body": "stale"})
	assertTaskToolCode(t, stale, task.CodeRevisionConflict)
}

func expireLease(t *testing.T, database, runID string) {
	t.Helper()

	db, err := sql.Open("sqlite3", database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`UPDATE run_leases SET expires_at='2000-01-01T00:00:00Z' WHERE run_id=?`, runID); err != nil {
		t.Fatal(err)
	}
}

func showRun(t *testing.T, session *mcp.ClientSession, runID string) runmodel.Show {
	t.Helper()

	var shown RunShowResult
	decodeStructured(t, callTool(t, session, "run_show", map[string]any{"run_id": runID}), &shown)
	if shown.Show == nil {
		t.Fatalf("run_show = %+v", shown)
	}

	return *shown.Show
}

package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vtimame/istok.sh/internal/task"
)

func TestMCPTaskStartContinuesItsRunWithTheHandoff(t *testing.T) {
	database := filepath.Join(t.TempDir(), "start.db")
	root := t.TempDir()
	_, session := newSessionAtDatabaseWithActor(t, Worker, root, database, "start-agent")
	initProject(t, session, "start")

	first := startTask(t, session, map[string]any{"title": "survive compaction", "acceptance_criteria": "the next session knows where we stopped"})
	if first.Resumed || first.Task == nil || first.Run == nil || first.Snapshot == nil || first.Handoff == nil {
		t.Fatalf("first task_start = %+v", first)
	}
	if first.Task.Title != "survive compaction" || first.Handoff.Handoff != nil || len(first.Handoff.Notes) != 0 {
		t.Fatalf("first task_start task %+v handoff %+v", first.Task, first.Handoff)
	}

	taskID := first.Task.ID
	callTaskResult(t, session, "task_progress", map[string]any{"task_id": taskID, "body": "before the handoff"})
	callTaskResult(t, session, "task_handoff", map[string]any{"task_id": taskID, "note": "done: schema\nnext: wire the CLI"})
	callTaskResult(t, session, "task_progress", map[string]any{"task_id": taskID, "body": "after the handoff"})

	expireLease(t, database, first.Run.ID)

	again := startTask(t, session, map[string]any{"task_id": taskID})
	if !again.Resumed || again.Run.ID != first.Run.ID || again.Run.LeaseID != first.Run.LeaseID {
		t.Fatalf("second task_start did not continue the run: first %+v again %+v", first.Run, again.Run)
	}
	if !again.Run.ExpiresAt.After(time.Now()) || again.Notice != "" {
		t.Fatalf("second task_start did not revive the lease: run %+v notice %q", again.Run, again.Notice)
	}
	if again.Snapshot == nil || again.Snapshot.ID != first.Snapshot.ID {
		t.Fatalf("second task_start snapshot = %+v, want %s", again.Snapshot, first.Snapshot.ID)
	}

	brief := again.Handoff
	if brief.Handoff == nil || brief.Handoff.Type != task.EventHandoff || brief.Handoff.Body != "done: schema\nnext: wire the CLI" {
		t.Fatalf("handoff = %+v", brief.Handoff)
	}
	if len(brief.Notes) != 1 || brief.Notes[0].Body != "after the handoff" {
		t.Fatalf("notes after the handoff = %+v", brief.Notes)
	}
	if brief.ActiveRun == nil || brief.ActiveRun.ID != first.Run.ID || brief.ActiveRun.LeaseExpired {
		t.Fatalf("active run = %+v", brief.ActiveRun)
	}
}

func TestMCPTaskStartLeavesAnotherAgentsRunAlone(t *testing.T) {
	database := filepath.Join(t.TempDir(), "other.db")
	root := t.TempDir()
	_, owner := newSessionAtDatabaseWithActor(t, Worker, root, database, "owner-agent")
	initProject(t, owner, "other")
	_, other := newSessionAtDatabaseWithActor(t, Worker, root, database, "other-agent")

	started := startTask(t, owner, map[string]any{"title": "owned work"})

	result := callTool(t, other, "task_start", map[string]any{"task_id": started.Task.ID})
	if !result.IsError {
		t.Fatalf("another agent started a task with a live run: %+v", result)
	}

	missing := callTool(t, owner, "task_start", map[string]any{})
	if !missing.IsError {
		t.Fatalf("task_start without task_id or title succeeded: %+v", missing)
	}
}

func startTask(t *testing.T, session *mcp.ClientSession, arguments map[string]any) TaskStartResult {
	t.Helper()

	result := callTool(t, session, "task_start", arguments)
	var started TaskStartResult
	decodeStructured(t, result, &started)
	if result.IsError {
		t.Fatalf("task_start %v = %+v", arguments, started)
	}

	return started
}

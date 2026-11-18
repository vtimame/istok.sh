package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/vtimame/istok.sh/internal/usage"
)

type memoryRecorder struct {
	mu     sync.Mutex
	calls  []usage.Call
	prunes int
	fail   error
}

func (r *memoryRecorder) Record(_ context.Context, call usage.Call) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail != nil {
		return r.fail
	}
	r.calls = append(r.calls, call)

	return nil
}

func (r *memoryRecorder) Prune(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.prunes++

	return nil
}

func (r *memoryRecorder) snapshot() ([]usage.Call, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]usage.Call(nil), r.calls...), r.prunes
}

func TestCallLogRecordsEveryToolCall(t *testing.T) {
	recorder := &memoryRecorder{}
	root := t.TempDir()
	_, session := newSessionWithRecorder(t, Worker, root, filepath.Join(t.TempDir(), "istok.db"), "mcp-agent", recorder)

	callTool(t, session, "health", nil)
	if result := callTool(t, session, "project_current", nil); !result.IsError {
		t.Fatalf("project_current on an unregistered root succeeded: %+v", result)
	}
	if result := callTool(t, session, "project_init", map[string]any{"name": "logged"}); result.IsError {
		t.Fatalf("project_init failed: %+v", result)
	}
	callTool(t, session, "task_list", nil)

	calls, prunes := recorder.snapshot()
	if prunes != 1 {
		t.Errorf("prunes = %d, want 1 per server", prunes)
	}
	if len(calls) != 4 {
		t.Fatalf("recorded %d calls, want 4: %+v", len(calls), calls)
	}

	wantTools := []string{"health", "project_current", "project_init", "task_list"}
	for i, call := range calls {
		if call.Tool != wantTools[i] {
			t.Errorf("call %d tool = %q, want %q", i, call.Tool, wantTools[i])
		}
		if call.SessionID != calls[0].SessionID || call.SessionID == "" {
			t.Errorf("call %d session = %q, want one non-empty session", i, call.SessionID)
		}
		if call.ActorID != "mcp-agent" || call.ClientName != "test" || call.ClientVersion != "test" {
			t.Errorf("call %d identity = actor %q client %q %q", i, call.ActorID, call.ClientName, call.ClientVersion)
		}
		if call.ID == "" || call.StartedAt.IsZero() || call.Duration < 0 {
			t.Errorf("call %d metadata = %+v", i, call)
		}
	}

	if calls[0].Outcome != usage.OK || calls[0].ErrorCode != "" {
		t.Errorf("health outcome = %q %q, want ok", calls[0].Outcome, calls[0].ErrorCode)
	}
	if calls[1].Outcome != usage.Error || calls[1].ErrorCode != "project_not_found" {
		t.Errorf("project_current outcome = %q %q, want error project_not_found", calls[1].Outcome, calls[1].ErrorCode)
	}
	if calls[0].ProjectID != "" || calls[1].ProjectID != "" {
		t.Errorf("calls before project_init have project %q, %q", calls[0].ProjectID, calls[1].ProjectID)
	}
	if calls[3].ProjectID == "" || calls[3].ProjectID != calls[2].ProjectID {
		t.Errorf("calls after project_init have project %q, %q", calls[2].ProjectID, calls[3].ProjectID)
	}
}

func TestCallLogFailureNeverFailsTheCall(t *testing.T) {
	recorder := &memoryRecorder{fail: errors.New("database is locked")}
	_, session := newSessionWithRecorder(t, Worker, t.TempDir(), filepath.Join(t.TempDir(), "istok.db"), "mcp-agent", recorder)

	if result := callTool(t, session, "health", nil); result.IsError {
		t.Fatalf("health failed when the call log failed: %+v", result)
	}
}

func TestFailureCodeReadsTheErrorEnvelope(t *testing.T) {
	if code := failureCode(errorTool(errors.New("boom")), nil); code == "" {
		t.Error("failureCode() of an error envelope is empty")
	}
	if code := failureCode(nil, errors.New("transport")); code != "protocol_error" {
		t.Errorf("failureCode() of a protocol error = %q", code)
	}
	if code := failureCode(nil, nil); code != "" {
		t.Errorf("failureCode() of a success = %q", code)
	}
}

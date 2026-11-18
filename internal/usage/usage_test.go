package usage

import (
	"reflect"
	"testing"
	"time"
)

func TestSummarizeCountsSessionsFunnelErrorsAndTools(t *testing.T) {
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	counts := []ToolCount{
		{SessionID: "a", ClientName: "claude-code", Tool: "project_current", Outcome: OK, Calls: 1},
		{SessionID: "a", ClientName: "claude-code", Tool: "task_create", Outcome: OK, Calls: 1},
		{SessionID: "a", ClientName: "claude-code", Tool: "task_progress", Outcome: OK, Calls: 3},
		{SessionID: "a", ClientName: "claude-code", Tool: "task_progress", Outcome: Error, ErrorCode: "revision_conflict", Calls: 2},
		{SessionID: "b", ClientName: "claude-code", Tool: "project_current", Outcome: OK, Calls: 2},
		{SessionID: "b", ClientName: "claude-code", Tool: "task_claim", Outcome: OK, Calls: 1},
		{SessionID: "b", ClientName: "claude-code", Tool: "run_validate", Outcome: OK, Calls: 1},
		{SessionID: "c", ClientName: "", Tool: "health", Outcome: Error, ErrorCode: "internal", Calls: 1},
	}

	report := Summarize(since, counts)

	if report.Since != since || report.Sessions != 3 || report.Calls != 12 || report.Failed != 3 {
		t.Fatalf("totals = since %v sessions %d calls %d failed %d", report.Since, report.Sessions, report.Calls, report.Failed)
	}

	wantClients := []NamedCount{{Name: "claude-code", Count: 2}, {Name: "unknown", Count: 1}}
	if !reflect.DeepEqual(report.Clients, wantClients) {
		t.Errorf("clients = %+v, want %+v", report.Clients, wantClients)
	}

	wantFunnel := []StepCount{
		{Step: "project", Sessions: 2},
		{Step: "find or create task", Sessions: 1},
		{Step: "claim", Sessions: 1},
		{Step: "progress", Sessions: 1},
		{Step: "heartbeat", Sessions: 0},
		{Step: "validate", Sessions: 1},
		{Step: "finish run", Sessions: 0},
		{Step: "complete task", Sessions: 0},
	}
	if !reflect.DeepEqual(report.Funnel, wantFunnel) {
		t.Errorf("funnel = %+v, want %+v", report.Funnel, wantFunnel)
	}

	wantErrors := []NamedCount{{Name: "revision_conflict", Count: 2}, {Name: "internal", Count: 1}}
	if !reflect.DeepEqual(report.Errors, wantErrors) {
		t.Errorf("errors = %+v, want %+v", report.Errors, wantErrors)
	}

	if report.Tools[0] != (ToolTotal{Tool: "task_progress", Calls: 5, Failed: 2, Sessions: 1}) {
		t.Errorf("top tool = %+v", report.Tools[0])
	}
	if report.Tools[1] != (ToolTotal{Tool: "project_current", Calls: 3, Failed: 0, Sessions: 2}) {
		t.Errorf("second tool = %+v", report.Tools[1])
	}
	if len(report.Tools) != 6 {
		t.Errorf("tools = %d, want 6", len(report.Tools))
	}
}

func TestSummarizeEmptyLogReturnsEmptyCollections(t *testing.T) {
	report := Summarize(time.Time{}, nil)

	if report.Sessions != 0 || report.Calls != 0 {
		t.Fatalf("report = %+v, want no sessions", report)
	}
	if report.Clients == nil || report.Errors == nil || report.Tools == nil {
		t.Fatalf("collections must be empty, not nil: %+v", report)
	}
	if len(report.Funnel) != len(Funnel) {
		t.Fatalf("funnel has %d steps, want %d", len(report.Funnel), len(Funnel))
	}
}

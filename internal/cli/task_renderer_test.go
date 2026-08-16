package cli

import (
	"strings"
	"testing"

	"s26.dev/istok-cli/internal/task"
)

func TestTaskListStateIsDerivedFromRunThenDependencyThenOpen(t *testing.T) {
	for _, tc := range []struct {
		name      string
		item      task.TaskListItem
		wantState string
	}{
		{
			name: "in progress wins over blocked",
			item: task.TaskListItem{
				Task:         task.Task{Status: task.StatusOpen},
				HasActiveRun: true,
				ActiveBlockers: []task.BlockerSummary{
					{Number: 1},
				},
			},
			wantState: "IN PROGRESS",
		},
		{
			name: "blocked by status",
			item: task.TaskListItem{
				Task: task.Task{Status: task.StatusBlocked},
			},
			wantState: "BLOCKED",
		},
		{
			name: "blocked by blockers",
			item: task.TaskListItem{
				Task: task.Task{Status: task.StatusOpen},
				ActiveBlockers: []task.BlockerSummary{
					{Number: 7},
				},
			},
			wantState: "BLOCKED",
		},
		{
			name: "ready default",
			item: task.TaskListItem{
				Task: task.Task{Status: task.StatusOpen},
			},
			wantState: "READY",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskListState(tc.item); got != tc.wantState {
				t.Fatalf("taskListState(%+v) = %q, want %q", tc.item, got, tc.wantState)
			}
		})
	}
}

func TestTaskListRendererReturnsDashWhenNoBlockers(t *testing.T) {
	got := taskListBlockedBy(task.TaskListItem{})
	if got != "—" {
		t.Fatalf("taskListBlockedBy() = %q, want em dash", got)
	}
}

func TestTaskListRendererAppendsNewlineForNonEmptyTables(t *testing.T) {
	got := taskListTableRenderer{}.RenderTaskList([]task.TaskListItem{
		{Task: task.Task{Number: 3, Title: "build"}},
	})
	if got == "No open tasks\n" {
		t.Fatalf("unexpected empty output: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("table output = %q", got)
	}
}

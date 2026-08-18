package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/vtimame/istok.sh/internal/task"
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
			wantState: "in progress",
		},
		{
			name: "blocked by status",
			item: task.TaskListItem{
				Task: task.Task{Status: task.StatusBlocked},
			},
			wantState: "blocked",
		},
		{
			name: "blocked by blockers",
			item: task.TaskListItem{
				Task: task.Task{Status: task.StatusOpen},
				ActiveBlockers: []task.BlockerSummary{
					{Number: 7},
				},
			},
			wantState: "blocked",
		},
		{
			name: "ready default",
			item: task.TaskListItem{
				Task: task.Task{Status: task.StatusOpen},
			},
			wantState: "ready",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.ToLower(taskListState(tc.item)); got != tc.wantState {
				t.Fatalf("taskListState(%+v) = %q, want %q", tc.item, got, tc.wantState)
			}
		})
	}
}

func TestTaskListRendererReturnsDashWhenNoBlockers(t *testing.T) {
	got := stripANSI(taskListBlockedBy(task.TaskListItem{}))
	if got != "—" {
		t.Fatalf("taskListBlockedBy() = %q, want em dash", got)
	}
}

func TestTaskListRendererAppendsNewlineForNonEmptyTables(t *testing.T) {
	got := humanTaskRenderer{}.RenderTaskList([]task.TaskListItem{
		{Task: task.Task{Number: 3, Title: "build"}},
	})
	if got == "No open tasks\n" {
		t.Fatalf("unexpected empty output: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("table output = %q", got)
	}
}

func TestTaskShowRendererOutputsAllSectionsWithSortedRelationsAndEvents(t *testing.T) {
	deleted := time.Now().UTC()
	earlier := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	later := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	show := task.Show{
		Task: task.Task{
			Number:             42,
			Revision:           11,
			Status:             task.StatusDone,
			Title:              "target",
			AcceptanceCriteria: "",
			Notes:              "notes",
		},
		Blockers: []task.TaskSummary{
			{Number: 10, Title: "first blocker", Status: task.StatusOpen},
			{Number: 2, Title: "second blocker", Status: task.StatusBlocked},
			{Number: 4, Title: "deleted blocker", DeletedAt: &deleted, Status: task.StatusOpen},
		},
		Dependents: []task.TaskSummary{
			{Number: 9, Title: "dependent one", Status: task.StatusDone},
			{Number: 3, Title: "dependent two", Status: task.StatusOpen},
		},
		Events: []task.Event{
			{CreatedAt: later, Type: "progress", Actor: task.ActorSnapshot{ID: "1", Kind: "cli", Name: "Alice"}, TaskRevision: 2, Body: ""},
			{CreatedAt: earlier, Type: "commented", Actor: task.ActorSnapshot{ID: "1", Kind: "cli", Name: "Alice"}, TaskRevision: 1, Body: "note"},
			{CreatedAt: later.Add(time.Hour), Type: "updated", Actor: task.ActorSnapshot{ID: "1", Kind: "cli", Name: "Alice"}, TaskRevision: 3, Body: "criteria"},
		},
	}

	got := humanTaskRenderer{}.RenderTaskShow(show)
	plain := stripANSI(got)

	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("show output is not ANSI-colored: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("show output does not end with newline: %q", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Fatalf("show output has extra newline: %q", got)
	}

	mustContain(t, plain, "Task · #42")
	mustContain(t, plain, "State:")
	mustContain(t, plain, "DONE")
	mustContain(t, plain, "Revision:")
	mustContain(t, plain, "r11")
	mustContain(t, plain, "Description")
	mustContain(t, plain, "Acceptance criteria")
	mustContain(t, plain, "Notes")
	mustContain(t, plain, "Relations")
	mustContain(t, plain, "History")
	mustContain(t, plain, "OPEN")
	mustContain(t, plain, "BLOCKED")
	mustContain(t, plain, "DONE")
	mustContain(t, plain, "DELETED")

	index2 := strings.Index(plain, "second blocker")
	index4 := strings.Index(plain, "deleted blocker")
	index10 := strings.Index(plain, "first blocker")
	if index2 == -1 || index4 == -1 || index10 == -1 || !(index2 < index4 && index4 < index10) {
		t.Fatalf("blocker rows are not sorted by number: %q", plain)
	}
	index3 := strings.Index(plain, "dependent two")
	index9 := strings.Index(plain, "dependent one")
	if index3 == -1 || index9 == -1 || index3 >= index9 {
		t.Fatalf("dependent rows are not sorted by number: %q", plain)
	}

	if !strings.Contains(plain, "Alice") {
		t.Fatalf("missing actor formatting: %q", plain)
	}
	if !strings.Contains(plain, earlier.UTC().Format("2006-01-02 10:00Z")) {
		t.Fatalf("missing event timestamp: %q", plain)
	}
	progressIndex := strings.Index(plain, "progress")
	commentIndex := strings.Index(plain, "commented")
	updatedIndex := strings.Index(plain, "updated")
	if progressIndex == -1 || commentIndex == -1 || updatedIndex == -1 || !(progressIndex < commentIndex && commentIndex < updatedIndex) {
		t.Fatalf("event rows do not preserve input order: %q", plain)
	}
	if strings.Count(plain, "—") == 0 {
		t.Fatalf("missing em dash for empty details: %q", plain)
	}

	sorted := sortedTaskSummaries(show.Blockers)
	if sorted[0].Number != 2 || sorted[1].Number != 4 || sorted[2].Number != 10 {
		t.Fatalf("blocked summary sort = %#v", sorted)
	}
	if sorted := sortedTaskSummaries(show.Dependents); sorted[0].Number != 3 || sorted[1].Number != 9 {
		t.Fatalf("dependent summary sort = %#v", sorted)
	}
}

func TestTaskShowRendererUsesDashForMissingOptionalAndRelationValues(t *testing.T) {
	got := humanTaskRenderer{}.RenderTaskShow(task.Show{
		Task: task.Task{Number: 5, Revision: 1, Status: task.StatusOpen, Title: "missing"},
	})
	plain := stripANSI(got)

	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("show output is not ANSI-colored: %q", got)
	}
	mustContain(t, plain, "Task · #5")
	mustContain(t, plain, "State")
	mustContain(t, plain, "Revision:")
	mustContain(t, plain, "r1")
	mustContain(t, plain, "Description")
	mustContain(t, plain, "Acceptance criteria")
	mustContain(t, plain, "Notes")
	mustContain(t, plain, "Relations")
	mustContain(t, plain, "History")
	mustContain(t, plain, "—")

	mustContain(t, plain, "—")
	mustContain(t, plain, "READY")

	if strings.Count(plain, "—") == 0 {
		t.Fatalf("missing em dash for optional values: %q", plain)
	}
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("show output %q does not contain %q", got, want)
	}
}

package cli

import (
	"strings"
	"testing"
	"time"

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

	if strings.Contains(got, "\x1b[") {
		t.Fatalf("show output has ANSI sequence: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("show output does not end with newline: %q", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Fatalf("show output has extra newline: %q", got)
	}

	mustContain(t, got, "Task #42")
	mustContain(t, got, "State: DONE")
	mustContain(t, got, "Revision: 11")
	mustContain(t, got, "Description:")
	mustContain(t, got, "Acceptance criteria:")
	mustContain(t, got, "Notes:")
	mustContain(t, got, "Blockers:")
	mustContain(t, got, "Dependents:")
	mustContain(t, got, "Event history:")
	mustContain(t, got, "OPEN")
	mustContain(t, got, "BLOCKED")
	mustContain(t, got, "DONE")
	mustContain(t, got, "DELETED")

	index2 := strings.Index(got, "second blocker")
	index4 := strings.Index(got, "deleted blocker")
	index10 := strings.Index(got, "first blocker")
	if index2 == -1 || index4 == -1 || index10 == -1 || !(index2 < index4 && index4 < index10) {
		t.Fatalf("blocker rows are not sorted by number: %q", got)
	}
	index3 := strings.Index(got, "dependent two")
	index9 := strings.Index(got, "dependent one")
	if index3 == -1 || index9 == -1 || index3 >= index9 {
		t.Fatalf("dependent rows are not sorted by number: %q", got)
	}

	if !strings.Contains(got, "Alice (cli)") {
		t.Fatalf("missing actor formatting: %q", got)
	}
	if !strings.Contains(got, earlier.UTC().Format(time.RFC3339)) {
		t.Fatalf("missing event timestamp: %q", got)
	}
	progressIndex := strings.Index(got, "progress")
	commentIndex := strings.Index(got, "commented")
	updatedIndex := strings.Index(got, "updated")
	if progressIndex == -1 || commentIndex == -1 || updatedIndex == -1 || !(progressIndex < commentIndex && commentIndex < updatedIndex) {
		t.Fatalf("event rows do not preserve input order: %q", got)
	}
	if strings.Count(got, "—") == 0 {
		t.Fatalf("missing em dash for empty details: %q", got)
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

	mustContain(t, got, "Description:\n—")
	mustContain(t, got, "Acceptance criteria:\n—")
	mustContain(t, got, "Notes:\n—")
	mustContain(t, got, "Blockers:\n—")
	mustContain(t, got, "Dependents:\n—")
	mustContain(t, got, "Event history:\n—")

	want := "Task #5\n" +
		"State: READY\n" +
		"Revision: 1\n" +
		"Title: missing\n\n" +
		"Description:\n—\n\n" +
		"Acceptance criteria:\n—\n\n" +
		"Notes:\n—\n\n" +
		"Blockers:\n—\n\n" +
		"Dependents:\n—\n\n" +
		"Event history:\n—\n"
	if got != want {
		t.Fatalf("RenderTaskShow() =\n%s\nwant:\n%s", got, want)
	}
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("show output %q does not contain %q", got, want)
	}
}

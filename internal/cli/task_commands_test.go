package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"

	"s26.dev/istok-cli/internal/task"
)

func TestTaskCommandParserIncludesWorkflowCommands(t *testing.T) {
	var command CLI
	parser, err := kong.New(&command, kong.Name("istok"))
	if err != nil {
		t.Fatalf("create parser: %v", err)
	}

	for _, args := range [][]string{
		{"task", "create", "--title", "task"},
		{"task", "list", "--status", "open"},
		{"task", "show", "1", "--json"},
		{"task", "ready", "--json"},
		{"task", "comment", "1", "--body", "comment"},
		{"task", "progress", "1", "--body", "progress"},
		{"task", "dependency", "add", "1", "--blocker", "2"},
		{"task", "dependency", "remove", "1", "--blocker", "2"},
	} {
		if _, err := parser.Parse(args); err != nil {
			t.Errorf("parse %v: %v", args, err)
		}
	}
}

func TestTaskWorkflowCommandsUseCurrentProjectAndProjectScopedNumbers(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	otherRoot := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")

	executeAt(t, root, database, "init", "--json")
	executeAt(t, otherRoot, database, "init", "--json")

	created := runTaskForTest(t, ctx, TaskCommand{Create: TaskCreateCommand{
		Title:              "root task",
		Description:        "description",
		AcceptanceCriteria: "acceptance",
		Notes:              "notes",
		Database:           database,
		JSON:               true,
	}}, "task create", root)
	rootTask := decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, created).Task

	otherCreated := runTaskForTest(t, ctx, TaskCommand{Create: TaskCreateCommand{
		Title:    "other task",
		Database: database,
		JSON:     true,
	}}, "task create", otherRoot)
	otherTask := decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, otherCreated).Task
	if rootTask.Number != 1 || otherTask.Number != 1 {
		t.Fatalf("task numbers = %d, %d; want project-local #1", rootTask.Number, otherTask.Number)
	}

	commented := runTaskForTest(t, ctx, TaskCommand{Comment: TaskCommentCommand{
		ID:       rootTask.Number,
		Body:     "comment body",
		Database: database,
		JSON:     true,
	}}, "task comment", root)
	commentedTask := decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, commented).Task
	if commentedTask.Revision <= rootTask.Revision {
		t.Fatalf("comment revision = %d, want greater than %d", commentedTask.Revision, rootTask.Revision)
	}

	progressed := runTaskForTest(t, ctx, TaskCommand{Progress: TaskProgressCommand{
		ID:       rootTask.Number,
		Body:     "progress body",
		Database: database,
		JSON:     true,
	}}, "task progress", root)
	progressedTask := decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, progressed).Task
	if progressedTask.Revision <= commentedTask.Revision {
		t.Fatalf("progress revision = %d, want greater than %d", progressedTask.Revision, commentedTask.Revision)
	}

	showOutput := runTaskForTest(t, ctx, TaskCommand{Show: TaskShowCommand{ID: rootTask.Number, Database: database, JSON: true}}, "task show", root)
	shown := decodeTaskResult[struct {
		Task task.Show `json:"task"`
	}](t, showOutput).Task
	if shown.Task.ID != rootTask.ID {
		t.Fatalf("show task id = %q, want %q", shown.Task.ID, rootTask.ID)
	}
	if len(shown.Events) < 3 {
		t.Fatalf("show events = %d, want create/comment/progress", len(shown.Events))
	}
	for _, event := range shown.Events {
		if event.Body != "comment body" && event.Body != "progress body" {
			continue
		}
		if event.Actor != cliTaskActor {
			t.Fatalf("event actor = %#v, want %#v", event.Actor, cliTaskActor)
		}
	}
}

func TestTaskListShowAndReadyJSONUseVersionedNonNullArrays(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")

	listOutput := runTaskForTest(t, ctx, TaskCommand{List: TaskListCommand{Database: database, JSON: true}}, "task list", root)
	assertTaskJSONList(t, listOutput, "tasks", 0)
	readyOutput := runTaskForTest(t, ctx, TaskCommand{Ready: TaskReadyCommand{Database: database, JSON: true}}, "task ready", root)
	assertTaskJSONList(t, readyOutput, "tasks", 0)

	created := runTaskForTest(t, ctx, TaskCommand{Create: TaskCreateCommand{Title: "ready task", Database: database, JSON: true}}, "task create", root)
	createdTask := decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, created).Task

	listOutput = runTaskForTest(t, ctx, TaskCommand{List: TaskListCommand{Status: []task.Status{task.StatusOpen}, Database: database, JSON: true}}, "task list", root)
	assertTaskJSONList(t, listOutput, "tasks", 1)
	readyOutput = runTaskForTest(t, ctx, TaskCommand{Ready: TaskReadyCommand{Database: database, JSON: true}}, "task ready", root)
	assertTaskJSONList(t, readyOutput, "tasks", 1)

	showOutput := runTaskForTest(t, ctx, TaskCommand{Show: TaskShowCommand{ID: createdTask.Number, Database: database, JSON: true}}, "task show", root)
	var showEnvelope struct {
		SchemaVersion string `json:"schema_version"`
		Result        struct {
			Task task.Show `json:"task"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(showOutput), &showEnvelope); err != nil {
		t.Fatalf("decode show JSON: %v\n%s", err, showOutput)
	}
	if showEnvelope.SchemaVersion != taskSchemaVersion || showEnvelope.Result.Task.Task.ID != createdTask.ID {
		t.Fatalf("show envelope = %#v", showEnvelope)
	}
}

func TestTaskDependencyCommandsMutateCurrentProjectOnly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	otherRoot := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")
	executeAt(t, otherRoot, database, "init", "--json")

	blocker := createTaskForTest(t, ctx, root, database, "blocker")
	blocked := createTaskForTest(t, ctx, root, database, "blocked")
	createTaskForTest(t, ctx, otherRoot, database, "other blocker")

	added := runTaskForTest(t, ctx, TaskCommand{Dependency: TaskDependencyCommand{Add: TaskDependencyAddCommand{
		ID: blocked.Number, Blocker: blocker.Number, Database: database, JSON: true,
	}}}, "task dependency add", root)
	addedTask := decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, added).Task
	if addedTask.ID != blocked.ID {
		t.Fatalf("dependency add changed %q, want %q", addedTask.ID, blocked.ID)
	}

	readyOutput := runTaskForTest(t, ctx, TaskCommand{Ready: TaskReadyCommand{Database: database, JSON: true}}, "task ready", root)
	assertTaskJSONList(t, readyOutput, "tasks", 1)

	removed := runTaskForTest(t, ctx, TaskCommand{Dependency: TaskDependencyCommand{Remove: TaskDependencyRemoveCommand{
		ID: blocked.Number, Blocker: blocker.Number, Database: database, JSON: true,
	}}}, "task dependency remove", root)
	removedTask := decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, removed).Task
	if removedTask.ID != blocked.ID {
		t.Fatalf("dependency remove changed %q, want %q", removedTask.ID, blocked.ID)
	}

	readyOutput = runTaskForTest(t, ctx, TaskCommand{Ready: TaskReadyCommand{Database: database, JSON: true}}, "task ready", root)
	assertTaskJSONList(t, readyOutput, "tasks", 2)
}

func createTaskForTest(t *testing.T, ctx context.Context, cwd, database, title string) task.Task {
	t.Helper()
	output := runTaskForTest(t, ctx, TaskCommand{Create: TaskCreateCommand{Title: title, Database: database, JSON: true}}, "task create", cwd)
	return decodeTaskResult[struct {
		Task task.Task `json:"task"`
	}](t, output).Task
}

func runTaskForTest(t *testing.T, ctx context.Context, command TaskCommand, commandName, cwd string) string {
	t.Helper()
	var output bytes.Buffer
	if err := runTaskCommand(ctx, command, commandName, cwd, &output); err != nil {
		t.Fatalf("%s: %v", commandName, err)
	}
	return output.String()
}

func decodeTaskResult[T any](t *testing.T, output string) T {
	t.Helper()
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Result        T      `json:"result"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode task JSON: %v\n%s", err, output)
	}
	if envelope.SchemaVersion != taskSchemaVersion {
		t.Fatalf("schema version = %q, want %q", envelope.SchemaVersion, taskSchemaVersion)
	}
	return envelope.Result
}

func assertTaskJSONList(t *testing.T, output, key string, want int) {
	t.Helper()
	var envelope struct {
		SchemaVersion string          `json:"schema_version"`
		Result        json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode task list JSON: %v\n%s", err, output)
	}
	if envelope.SchemaVersion != taskSchemaVersion {
		t.Fatalf("schema version = %q, want %q", envelope.SchemaVersion, taskSchemaVersion)
	}

	var result struct {
		Tasks []task.TaskListItem `json:"tasks"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode task list result: %v", err)
	}
	if result.Tasks == nil {
		t.Fatalf("%s is null, want []", key)
	}
	if len(result.Tasks) != want {
		t.Fatalf("%s count = %d, want %d", key, len(result.Tasks), want)
	}
}

func TestTaskCommandRejectsMissingRequiredFlags(t *testing.T) {
	var command CLI
	parser, err := kong.New(&command, kong.Name("istok"))
	if err != nil {
		t.Fatalf("create parser: %v", err)
	}

	for _, args := range [][]string{
		{"task", "create"},
		{"task", "comment", "1"},
		{"task", "progress", "1"},
		{"task", "dependency", "add", "1"},
	} {
		if _, err := parser.Parse(args); err == nil {
			t.Errorf("parse %v unexpectedly succeeded", args)
		}
	}
}

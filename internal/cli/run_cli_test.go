package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runmodel "s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

func TestRunCLIJSONKernelWorkflowAndImmutableSnapshot(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")

	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID

	contextAdded := executeAt(t, root, database,
		"context", "add", "Architecture",
		"--kind", "decision",
		"--body", "original snapshot body",
		"--json",
	)
	if contextAdded.err != nil {
		t.Fatalf("context add: %v", contextAdded.err)
	}
	contextRecord := decodeJSONResult[contextView](t, contextAdded.output).Record

	tasks, _ := newTaskRepositoryForTest(t, database)
	taskValue, err := tasks.Create(context.Background(), task.CreateInput{
		ProjectID: projectID,
		Title:     "run kernel workflow",
	}, task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	claimed := executeAt(t, root, database,
		"task", "claim", "1",
		"--base-branch", "main",
		"--base-commit", "0123456789abcdef",
		"--json",
	)
	if claimed.err != nil {
		t.Fatalf("task claim: %v", claimed.err)
	}
	claimedRun := decodeJSONResult[runmodel.Run](t, claimed.output)
	if !runmodel.IsUUIDv7(claimedRun.ID) || claimedRun.TaskID != taskValue.ID || claimedRun.Status != runmodel.StatusActive {
		t.Fatalf("claimed run = %+v", claimedRun)
	}

	listedTasks := executeAt(t, root, database, "task", "list")
	if listedTasks.err != nil {
		t.Fatalf("task list: %v", listedTasks.err)
	}
	if !strings.Contains(stripANSI(listedTasks.output), "IN PROGRESS") {
		t.Fatalf("task list does not derive active run state:\n%s", listedTasks.output)
	}

	updatedContext := executeAt(t, root, database,
		"context", "update", contextRecord.ID,
		"--expected-revision", "1",
		"--body", "changed after claim",
		"--json",
	)
	if updatedContext.err != nil {
		t.Fatalf("context update: %v", updatedContext.err)
	}

	shown := executeAt(t, root, database, "run", "show", claimedRun.ID, "--json")
	if shown.err != nil {
		t.Fatalf("run show: %v", shown.err)
	}
	show := decodeJSONResult[runmodel.Show](t, shown.output)
	if len(show.Snapshot.Records) != 1 {
		t.Fatalf("snapshot records = %+v", show.Snapshot.Records)
	}
	item := show.Snapshot.Records[0]
	if item.RecordID != contextRecord.ID || item.RecordRevision != 1 || item.Body != "original snapshot body" || item.ContentHash == "" {
		t.Fatalf("immutable snapshot item = %+v", item)
	}

	started := executeRunStartAt(t, root, database,
		"run", "execution", "start", claimedRun.ID,
		"--lease", claimedRun.LeaseID,
		"--cwd", ".",
		"--json",
		"go", "test", "./...",
	)
	if started.err != nil {
		t.Fatalf("execution start: %v", started.err)
	}
	execution := decodeJSONResult[runmodel.Execution](t, started.output)
	if execution.Status != runmodel.ExecutionRunning || len(execution.Argv) != 3 {
		t.Fatalf("started execution = %+v", execution)
	}

	finishedExecution := executeAt(t, root, database,
		"run", "execution", "finish", execution.ID,
		"--run", claimedRun.ID,
		"--expected-revision", "1",
		"--status", "succeeded",
		"--exit-code", "0",
		"--duration-ms", "25",
		"--json",
	)
	if finishedExecution.err != nil {
		t.Fatalf("execution finish: %v", finishedExecution.err)
	}
	execution = decodeJSONResult[runmodel.Execution](t, finishedExecution.output)
	if execution.Status != runmodel.ExecutionSucceeded || execution.Revision != 2 || execution.ExitCode == nil || *execution.ExitCode != 0 {
		t.Fatalf("finished execution = %+v", execution)
	}

	recorded := executeAt(t, root, database,
		"run", "validation", "record", execution.ID,
		"--run", claimedRun.ID,
		"--source", "attested",
		"--status", "passed",
		"--command", "go test ./...",
		"--summary", "tests passed",
		"--exit-code", "0",
		"--duration-ms", "25",
		"--json",
	)
	if recorded.err != nil {
		t.Fatalf("validation record: %v", recorded.err)
	}
	validation := decodeJSONResult[runmodel.Validation](t, recorded.output)
	if validation.ExecutionID != execution.ID || validation.Status != runmodel.ValidationStatusPassed {
		t.Fatalf("validation = %+v", validation)
	}

	finishedRun := executeAt(t, root, database,
		"run", "finish", claimedRun.ID,
		"--lease", claimedRun.LeaseID,
		"--expected-revision", "1",
		"--status", "succeeded",
		"--summary", "implemented and validated",
		"--json",
	)
	if finishedRun.err != nil {
		t.Fatalf("run finish: %v", finishedRun.err)
	}
	claimedRun = decodeJSONResult[runmodel.Run](t, finishedRun.output)
	if claimedRun.Status != runmodel.StatusSucceeded || claimedRun.Revision != 2 {
		t.Fatalf("finished run = %+v", claimedRun)
	}

	completed := executeAt(t, root, database,
		"task", "done", "1",
		"--expected-revision", "1",
		"--run-id", claimedRun.ID,
		"--validation-id", validation.ID,
		"--note", "kernel workflow complete",
		"--json",
	)
	if completed.err != nil {
		t.Fatalf("task done: %v", completed.err)
	}
	completion := decodeJSONResult[taskCompletionResult](t, completed.output)
	if completion.Task.Status != task.StatusDone || completion.Task.Revision != 2 || completion.Completion.ValidationID == nil {
		t.Fatalf("completion = %+v", completion)
	}
}

func TestRunCLISuccessRequiresEvidenceAndJSONErrorsAreVersioned(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")

	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID
	tasks, _ := newTaskRepositoryForTest(t, database)
	if _, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "needs evidence"}, task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}); err != nil {
		t.Fatal(err)
	}

	claimed := executeAt(t, root, database, "task", "claim", "1", "--json")
	if claimed.err != nil {
		t.Fatalf("task claim: %v", claimed.err)
	}
	runValue := decodeJSONResult[runmodel.Run](t, claimed.output)

	failed := executeAt(t, root, database,
		"run", "finish", runValue.ID,
		"--lease", runValue.LeaseID,
		"--expected-revision", "1",
		"--status", "succeeded",
		"--summary", "no evidence",
		"--json",
	)
	assertVersionedBusinessError(t, failed.err, string(runmodel.CodeEvidenceRequired))

	overridden := executeAt(t, root, database,
		"run", "finish", runValue.ID,
		"--lease", runValue.LeaseID,
		"--expected-revision", "1",
		"--status", "succeeded",
		"--summary", "operator accepted",
		"--allow-unvalidated",
		"--override-reason", "external validation unavailable",
		"--json",
	)
	if overridden.err != nil {
		t.Fatalf("run finish override: %v", overridden.err)
	}
	finished := decodeJSONResult[runmodel.Run](t, overridden.output)
	if finished.ValidationOverride != "external validation unavailable" {
		t.Fatalf("validation override = %q", finished.ValidationOverride)
	}
}

func TestRunHelpAndParseDoNotCreateDatabase(t *testing.T) {
	dataHome := t.TempDir()
	setXDGDataHome(t, dataHome)

	help := executeHelp(t, "run", "--help")
	if !strings.Contains(help, "execution") || !strings.Contains(help, "validation") {
		t.Fatalf("run help = %q", help)
	}

	for _, args := range [][]string{
		{"run", "execution", "start"},
		{"task", "claim", "not-a-number"},
	} {
		result := executeAt(t, t.TempDir(), "", args...)
		if result.err == nil {
			t.Fatalf("parse %v unexpectedly succeeded", args)
		}
	}

	if path := filepath.Join(dataHome, "istok", "istok.db"); fileExists(path) {
		t.Fatalf("database was created by help/parse at %s", path)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func executeRunStartAt(t *testing.T, cwd, database string, args ...string) executeResult {
	t.Helper()

	insertAt := len(args) - 3
	args = append(args[:insertAt], append([]string{"--database", database}, args[insertAt:]...)...)

	var output, errorOutput bytes.Buffer
	err := ExecuteAt(context.Background(), args, strings.NewReader(""), &output, &errorOutput, cwd)
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr for %q: %q", args, errorOutput.String())
	}

	return executeResult{output: output.String(), err: err}
}

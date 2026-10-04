package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	runmodel "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

func TestRunErrorCodeMapsClaimDependencies(t *testing.T) {
	contextErr := contextmodel.NewError(contextmodel.CodeConflict, "context conflict")
	indexErr := &indexingapp.Error{Cause: errors.New("index failed")}

	if got := runErrorCode(contextErr); got != string(contextmodel.CodeConflict) {
		t.Fatalf("context error code = %q", got)
	}
	if got := runErrorCode(indexErr); got != indexingapp.ErrorCode {
		t.Fatalf("index error code = %q", got)
	}
}

func TestCLIJSONErrorsIncludeStructuredContextBudgetDetails(t *testing.T) {
	contextErr := contextmodel.NewBudgetError("too many required records", contextmodel.BudgetFailure{
		Reason:                contextmodel.BudgetReasonDurableItems,
		Lane:                  contextmodel.LaneAlways,
		Action:                "retry with context_limit=15 or omit the explicit limit",
		RequiredItems:         15,
		MaxItems:              12,
		RequiredBytes:         12548,
		MaxBytes:              contextmodel.DefaultDurableBudgetBytes,
		RecordIDs:             []string{"record-a", "record-b"},
		SuggestedContextLimit: 15,
	})

	for name, err := range map[string]error{
		"context": jsonContextError(contextErr),
		"run":     runJSONError(contextErr),
	} {
		t.Run(name, func(t *testing.T) {
			var payload struct {
				Error struct {
					Details *contextmodel.BudgetFailure `json:"details"`
				} `json:"error"`
			}
			if unmarshalErr := json.Unmarshal([]byte(err.Error()), &payload); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			if payload.Error.Details == nil || !reflect.DeepEqual(payload.Error.Details, contextmodel.ErrorBudget(contextErr)) {
				t.Fatalf("structured error = %s", err)
			}
		})
	}
}

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
		"--delivery", "always",
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
	claimedRun := decodeJSONResultAtVersion[runmodel.Run](t, claimed.output, "2")
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
	show := decodeJSONResultAtVersion[runmodel.Show](t, shown.output, "2")
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
	execution := decodeJSONResultAtVersion[runmodel.Execution](t, started.output, "2")
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
	execution = decodeJSONResultAtVersion[runmodel.Execution](t, finishedExecution.output, "2")
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
	validation := decodeJSONResultAtVersion[runmodel.Validation](t, recorded.output, "2")
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
	claimedRun = decodeJSONResultAtVersion[runmodel.Run](t, finishedRun.output, "2")
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
	completion := decodeJSONResultAtVersion[taskCompletionResult](t, completed.output, "2")
	if completion.Task.Status != task.StatusDone || completion.Task.Revision != 2 || completion.Completion.ValidationID == nil {
		t.Fatalf("completion = %+v", completion)
	}
}

func TestTaskClaimAutomaticallySnapshotsLocalRetrieval(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	path := filepath.Join(root, "unique.go")
	if err := os.WriteFile(path, []byte("package unique\n\nfunc IstokUniqueClaimSymbol() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID
	tasks, _ := newTaskRepositoryForTest(t, database)
	if _, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "IstokUniqueClaimSymbol"}, task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	claimed := executeAt(t, root, database, "task", "claim", "1", "--json")
	if claimed.err != nil {
		t.Fatal(claimed.err)
	}
	runValue := decodeJSONResultAtVersion[runmodel.Run](t, claimed.output, "2")
	shown := executeAt(t, root, database, "run", "show", runValue.ID, "--json")
	if shown.err != nil {
		t.Fatal(shown.err)
	}
	before := decodeJSONResultAtVersion[runmodel.Show](t, shown.output, "2").Snapshot
	if len(before.Retrieval) == 0 || before.Retrieval[0].Visibility != "local_only" {
		t.Fatalf("retrieval = %#v", before.Retrieval)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	after := decodeJSONResultAtVersion[runmodel.Show](t, executeAt(t, root, database, "run", "show", runValue.ID, "--json").output, "2").Snapshot
	if after.Retrieval[0].Snippet != before.Retrieval[0].Snippet || after.Retrieval[0].ContentHash != before.Retrieval[0].ContentHash {
		t.Fatalf("snapshot changed: before=%#v after=%#v", before.Retrieval[0], after.Retrieval[0])
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
	runValue := decodeJSONResultAtVersion[runmodel.Run](t, claimed.output, "2")

	failed := executeAt(t, root, database,
		"run", "finish", runValue.ID,
		"--lease", runValue.LeaseID,
		"--expected-revision", "1",
		"--status", "succeeded",
		"--summary", "no evidence",
		"--json",
	)
	assertVersionedBusinessErrorAtVersion(t, failed.err, "2", string(runmodel.CodeEvidenceRequired))

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
	finished := decodeJSONResultAtVersion[runmodel.Run](t, overridden.output, "2")
	if finished.ValidationOverride != "external validation unavailable" {
		t.Fatalf("validation override = %q", finished.ValidationOverride)
	}
}

func TestRunCLIAbandon(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")

	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID
	tasks, _ := newTaskRepositoryForTest(t, database)
	if _, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "abandonable"}, task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}); err != nil {
		t.Fatal(err)
	}

	claimed := executeAt(t, root, database, "task", "claim", "1", "--json")
	if claimed.err != nil {
		t.Fatal(claimed.err)
	}
	runValue := decodeJSONResultAtVersion[runmodel.Run](t, claimed.output, "2")

	abandoned := executeAt(t, root, database,
		"run", "abandon", runValue.ID,
		"--expected-revision", "1",
		"--reason", "operator handoff",
		"--json",
	)
	if abandoned.err != nil {
		t.Fatal(abandoned.err)
	}
	value := decodeJSONResultAtVersion[runmodel.Run](t, abandoned.output, "2")
	if value.Status != runmodel.StatusAbandoned || value.FinishedBy == nil || value.FinishedBy.ID != "cli" {
		t.Fatalf("abandoned run = %+v", value)
	}

	ready := executeAt(t, root, database, "task", "ready", "--json")
	if ready.err != nil || !strings.Contains(ready.output, "abandonable") {
		t.Fatalf("ready tasks output=%q err=%v", ready.output, ready.err)
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

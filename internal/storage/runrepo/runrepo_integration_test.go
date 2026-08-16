package runrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/fx"

	contextapp "s26.dev/istok-cli/internal/application/context"
	runapp "s26.dev/istok-cli/internal/application/run"
	taskapp "s26.dev/istok-cli/internal/application/task"
	runmodel "s26.dev/istok-cli/internal/context"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/storage"
	"s26.dev/istok-cli/internal/storage/contextrepo"
	"s26.dev/istok-cli/internal/storage/projectrepo"
	"s26.dev/istok-cli/internal/storage/taskrepo"
	taskmodel "s26.dev/istok-cli/internal/task"
)

var testRunActor = run.ActorSnapshot{
	ID:   "local-runner",
	Kind: "user",
	Name: "Local Runner",
}

var testTaskActor = taskmodel.ActorSnapshot{
	ID:   "local-user",
	Kind: "user",
	Name: "Local User",
}

var testContextActor = runmodel.ActorSnapshot{
	ID:   "local-user",
	Kind: "user",
	Name: "Local User",
}

type runFixture struct {
	db             *sql.DB
	projects       *project.Service
	tasks          *taskrepo.Repository
	contextRecords *contextrepo.Repository
	runs           *Repository
	runService     *runapp.Service
}

func newRunFixtureAtPath(t *testing.T, database string, clock func() time.Time) *runFixture {
	t.Helper()

	var db *sql.DB
	var service *project.Service
	var tasks *taskrepo.Repository
	var records *contextrepo.Repository

	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: database}),
		fx.Provide(projectrepo.New),
		fx.Provide(func(repository *projectrepo.Repository) project.Repository { return repository }),
		fx.Provide(project.NewService),
		fx.Provide(taskrepo.New),
		fx.Provide(contextrepo.New),
		fx.Invoke(func(
			database *sql.DB,
			projectService *project.Service,
			taskRepository *taskrepo.Repository,
			recordRepository *contextrepo.Repository,
		) {
			db = database
			service = projectService
			tasks = taskRepository
			records = recordRepository
		}),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Error(err)
		}
	})

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConns = %d, want 1", got)
	}

	var runs *Repository
	if clock == nil {
		runs = New(db)
	} else {
		runs = NewWithClock(db, clock)
	}

	return &runFixture{
		db:             db,
		projects:       service,
		tasks:          tasks,
		contextRecords: records,
		runs:           runs,
		runService:     runapp.NewService(runs, tasks, contextapp.NewService(records)),
	}
}

func newRunFixture(t *testing.T) *runFixture {
	t.Helper()
	return newRunFixtureAtPath(t, filepath.Join(t.TempDir(), "istok.db"), nil)
}

func newRunFixtureWithClock(t *testing.T, clock func() time.Time) *runFixture {
	t.Helper()
	return newRunFixtureAtPath(t, filepath.Join(t.TempDir(), "istok.db"), clock)
}

func newProject(t *testing.T, f *runFixture, name string) project.Project {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("project-%s", name))
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := f.projects.Init(context.Background(), path, name)
	if err != nil {
		t.Fatal(err)
	}

	return result.Project
}

func newTask(t *testing.T, f *runFixture, projectID string, title string) taskmodel.Task {
	t.Helper()
	value, err := f.tasks.Create(context.Background(), taskmodel.CreateInput{ProjectID: projectID, Title: title}, testTaskActor)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newContextRecord(
	t *testing.T,
	f *runFixture,
	projectID string,
	title string,
	body string,
	kind runmodel.Kind,
) runmodel.ProjectContextRecord {
	t.Helper()
	value, err := f.contextRecords.Create(context.Background(), runmodel.CreateInput{
		ProjectID:   projectID,
		Kind:        kind,
		Title:       title,
		Body:        body,
		Source:      runmodel.SourceUser,
		Visibility:  runmodel.VisibilityShared,
		Sensitivity: runmodel.SensitivityNormal,
	}, testContextActor)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func newSnapshot(t *testing.T, projectID string, hashes map[string]string, records ...runmodel.ProjectContextRecord) run.ContextSnapshot {
	t.Helper()

	items := make([]runmodel.ContextPackageItem, 0, len(records))
	for i := range records {
		record := records[i]
		hash := hashes[record.ID]
		if strings.TrimSpace(hash) == "" {
			hash = contextRecordHash(record)
		}

		items = append(items, runmodel.ContextPackageItem{
			RecordID:       record.ID,
			RecordRevision: record.Revision,
			ContentHash:    hash,
			Kind:           record.Kind,
			Source:         record.Source,
			Visibility:     record.Visibility,
			Sensitivity:    record.Sensitivity,
			Title:          record.Title,
			Body:           record.Body,
			Snippet:        record.Body,
			Tags:           append([]string(nil), record.Tags...),
		})
	}

	snapshotID := mustRunID(t)
	value, err := run.NewContextSnapshot(snapshotID, runmodel.ContextPackage{
		SchemaVersion: run.ContextSnapshotSchemaVersion,
		ProjectID:     projectID,
		GeneratedAt:   time.Unix(1, 0).UTC(),
		Records:       items,
	}, projectID)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func claimRun(t *testing.T, f *runFixture, taskID string, snapshot run.ContextSnapshot, ids ...string) run.Run {
	t.Helper()

	claim := run.ClaimRecord{
		RunID:      idsOrDefault(0, ids, mustRunID(t)),
		TaskID:     taskID,
		EventID:    idsOrDefault(1, ids, mustRunID(t)),
		Snapshot:   snapshot,
		BaseBranch: "main",
		BaseCommit: "abc123",
		Actor:      testRunActor,
	}

	value, err := f.runs.Claim(context.Background(), claim)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func claimRunViaService(
	t *testing.T,
	f *runFixture,
	projectID string,
	taskID string,
	input run.ClaimInput,
) run.Run {
	t.Helper()

	selector := taskmodel.Selector{ProjectID: projectID, ID: taskID}
	value, err := f.runService.Claim(context.Background(), selector, input, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func startExecution(t *testing.T, f *runFixture, runID string) run.Execution {
	t.Helper()
	claimed, err := f.runs.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	value, err := f.runs.StartExecution(context.Background(), run.StartExecutionInput{
		ID:      mustRunID(t),
		RunID:   runID,
		LeaseID: claimed.LeaseID,
		Argv:    []string{"echo", "ok"},
		CWD:     "/tmp",
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func finishExecution(t *testing.T, f *runFixture, value run.Execution, status run.ExecutionStatus) run.Execution {
	t.Helper()
	code := 0
	if status != run.ExecutionSucceeded {
		code = 1
	}
	finished, err := f.runs.FinishExecution(context.Background(), run.FinishExecutionInput{
		ExecutionID:      value.ID,
		ExpectedRevision: value.Revision,
		Status:           status,
		ExitCode:         &code,
		DurationMS:       int64Ptr(42),
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return finished
}

func recordValidation(t *testing.T, f *runFixture, executionID string, status run.ValidationStatus) run.Validation {
	t.Helper()
	exitCode := 0
	validation, err := f.runs.RecordValidation(context.Background(), run.RecordValidationInput{
		ID:          mustRunID(t),
		ExecutionID: executionID,
		Source:      run.ValidationSourceAttested,
		Status:      status,
		Command:     "echo ok",
		ExitCode:    &exitCode,
		Summary:     "ok",
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return validation
}

func finishRun(t *testing.T, f *runFixture, value run.Run, status run.Status, allowUnvalidated bool, override string) run.Run {
	t.Helper()
	value, err := f.runs.FinishRun(context.Background(), run.FinishRunInput{
		RunID:            value.ID,
		LeaseID:          value.LeaseID,
		EventID:          mustRunID(t),
		ExpectedRevision: value.Revision,
		Status:           status,
		ResultSummary:    "result",
		AllowUnvalidated: allowUnvalidated,
		OverrideReason:   override,
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func completeTask(
	t *testing.T,
	f *runFixture,
	taskValue taskmodel.Task,
	runID *string,
	validationID *string,
	override string,
) (taskmodel.Task, run.Completion) {
	t.Helper()

	_, completion, err := f.runs.CompleteTask(context.Background(), run.CompletionRecord{
		ID:                   mustRunID(t),
		TaskID:               taskValue.ID,
		EventID:              mustRunID(t),
		ExpectedTaskRevision: taskValue.Revision,
		RunID:                runID,
		ValidationID:         validationID,
		Note:                 "done",
		OverrideReason:       override,
		Actor:                testRunActor,
		CreatedAt:            time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	completedTaskValue, err := f.tasks.Get(context.Background(), taskValue.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	return completedTaskValue, completion
}

func hasActiveRun(t *testing.T, f *runFixture, taskID string) bool {
	t.Helper()
	value, err := f.runs.HasActiveRun(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()

	row := db.QueryRow(query, args...)
	var value int
	if err := row.Scan(&value); err != nil {
		t.Fatalf("scan count: %v", err)
	}

	return value
}

func mustRunID(t *testing.T) string {
	t.Helper()
	value, err := run.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func contextRecordHash(value runmodel.ProjectContextRecord) string {
	tags := append([]string(nil), value.Tags...)
	sort.Strings(tags)
	payload := struct {
		Kind        runmodel.Kind        `json:"kind"`
		Title       string               `json:"title"`
		Body        string               `json:"body"`
		Tags        []string             `json:"tags"`
		Source      runmodel.Source      `json:"source"`
		Visibility  runmodel.Visibility  `json:"visibility"`
		Sensitivity runmodel.Sensitivity `json:"sensitivity"`
	}{
		Kind:        value.Kind,
		Title:       value.Title,
		Body:        value.Body,
		Tags:        tags,
		Source:      value.Source,
		Visibility:  value.Visibility,
		Sensitivity: value.Sensitivity,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func hashOf(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func idsOrDefault(index int, values []string, fallback string) string {
	if len(values) > index && values[index] != "" {
		return values[index]
	}
	return fallback
}

func int64Ptr(value int64) *int64 {
	return &value
}

func newClock(start time.Time, step time.Duration) func() time.Time {
	value := start
	mutex := sync.Mutex{}

	return func() time.Time {
		mutex.Lock()
		defer mutex.Unlock()

		current := value
		value = value.Add(step)
		return current
	}
}

func snapshotItemByRecordID(snapshot run.ContextSnapshot, recordID string) run.ContextSnapshotItem {
	for _, item := range snapshot.Records {
		if item.RecordID == recordID {
			return item
		}
	}
	return run.ContextSnapshotItem{}
}

func TestRunRepoComprehensiveHappyPath(t *testing.T) {
	f := newRunFixture(t)
	clock := newClock(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), time.Minute)
	_ = clock
	projectValue := newProject(t, f, "happy")
	targetTask := newTask(t, f, projectValue.ID, "implement happy path")

	recordA := newContextRecord(t, f, projectValue.ID, "decision-a", "first", runmodel.KindDecision)
	recordB := newContextRecord(t, f, projectValue.ID, "instruction-b", "second", runmodel.KindInstruction)

	claimed := claimRunViaService(t, f, projectValue.ID, targetTask.ID, run.ClaimInput{
		ContextLimit: 20,
		BaseBranch:   "main",
		BaseCommit:   "abc123",
	})
	if claimed.Status != run.StatusActive {
		t.Fatalf("unexpected claim status: %s", claimed.Status)
	}

	execution := startExecution(t, f, claimed.ID)
	execution = finishExecution(t, f, execution, run.ExecutionSucceeded)
	validation := recordValidation(t, f, execution.ID, run.ValidationStatusPassed)
	claimed = finishRun(t, f, claimed, run.StatusSucceeded, false, "")
	completedTask, completion := completeTask(t, f, targetTask, &claimed.ID, &validation.ID, "")

	show, err := f.runs.ShowRun(context.Background(), claimed.ID)
	if err != nil {
		t.Fatal(err)
	}

	if claimed.Revision != 2 {
		t.Fatalf("claimed revision = %d, want 2", claimed.Revision)
	}
	if claimed.ValidationOverride != "" {
		t.Fatalf("unexpected validation override = %q", claimed.ValidationOverride)
	}
	if claimed.ResultSummary != "result" {
		t.Fatalf("run result summary = %q, want %q", claimed.ResultSummary, "result")
	}
	if show.Snapshot.ID != claimed.ContextSnapshotID {
		t.Fatalf("snapshot mismatch: %s != %s", show.Snapshot.ID, claimed.ContextSnapshotID)
	}
	if len(show.Executions) != 1 || show.Executions[0].Status != run.ExecutionSucceeded {
		t.Fatalf("executions = %#v", show.Executions)
	}
	if len(show.Validations) != 1 || show.Validations[0].Status != run.ValidationStatusPassed {
		t.Fatalf("validations = %#v", show.Validations)
	}

	for _, record := range []runmodel.ProjectContextRecord{recordA, recordB} {
		item := snapshotItemByRecordID(show.Snapshot, record.ID)
		if item.RecordID == "" {
			t.Fatalf("snapshot missing record %q", record.ID)
		}
		if item.RecordRevision != record.Revision {
			t.Fatalf("record %q revision = %d, want %d", record.ID, item.RecordRevision, record.Revision)
		}
		if item.ContentHash != contextRecordHash(record) {
			t.Fatalf("record %q hash = %q, want %q", record.ID, item.ContentHash, contextRecordHash(record))
		}
		if item.Body != record.Body {
			t.Fatalf("record %q body = %q, want %q", record.ID, item.Body, record.Body)
		}
	}

	if completedTask.Status != taskmodel.StatusDone {
		t.Fatalf("task status after completion = %s", completedTask.Status)
	}
	if completedTask.Revision != 2 {
		t.Fatalf("task revision = %d, want 2", completedTask.Revision)
	}
	if completion.RunID == nil || *completion.RunID != claimed.ID {
		t.Fatalf("completion run id = %#v", completion.RunID)
	}
	if completion.Note != "done" {
		t.Fatalf("completion note = %q, want %q", completion.Note, "done")
	}

	events, err := f.tasks.Events(context.Background(), targetTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("events count = %d", len(events))
	}
	if events[0].Type != "created" || events[1].Type != "claimed" || events[2].Type != "run_finished" || events[3].Type != "completed" {
		t.Fatalf("unexpected event sequence: %#v", events)
	}
	if events[1].Body != claimed.ID {
		t.Fatalf("claim event body = %q, want run id %q", events[1].Body, claimed.ID)
	}
	if events[2].Body != claimed.ID+"\nresult" {
		t.Fatalf("run finish event body = %q", events[2].Body)
	}
	if events[3].Body != completion.ID+"\ndone" {
		t.Fatalf("completion event body = %q", events[3].Body)
	}

	if hasActiveRun(t, f, targetTask.ID) {
		t.Fatal("run should be terminal after completion")
	}
}

func TestRunRepoClaimRejectsUnclaimableTasksAndRollsBackArtifacts(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, *runFixture, taskmodel.Task) error
		code  run.Code
	}{
		{
			name: "blocked",
			setup: func(t *testing.T, f *runFixture, taskValue taskmodel.Task) error {
				_, err := f.tasks.Block(context.Background(), taskValue.ID, taskValue.Revision, "blocked", testTaskActor)
				return err
			},
			code: run.CodeInvalidTransition,
		},
		{
			name: "done",
			setup: func(t *testing.T, f *runFixture, taskValue taskmodel.Task) error {
				value := claimRun(t, f, taskValue.ID, newSnapshot(t, taskValue.ProjectID, nil,
					newContextRecord(t, f, taskValue.ProjectID, "record", "body", runmodel.KindDecision),
				))
				execution := startExecution(t, f, value.ID)
				execution = finishExecution(t, f, execution, run.ExecutionSucceeded)
				validation := recordValidation(t, f, execution.ID, run.ValidationStatusPassed)
				value = finishRun(t, f, value, run.StatusSucceeded, false, "")
				_, _, err := f.runs.CompleteTask(context.Background(), run.CompletionRecord{
					ID:                   mustRunID(t),
					TaskID:               taskValue.ID,
					EventID:              mustRunID(t),
					ExpectedTaskRevision: taskValue.Revision,
					RunID:                &value.ID,
					ValidationID:         &validation.ID,
					Note:                 "done",
					Actor:                testRunActor,
					CreatedAt:            time.Unix(0, 0).UTC(),
				})
				return err
			},
			code: run.CodeInvalidTransition,
		},
		{
			name: "deleted",
			setup: func(_ *testing.T, f *runFixture, taskValue taskmodel.Task) error {
				_, err := f.tasks.Delete(context.Background(), taskValue.ID, taskValue.Revision, testTaskActor)
				return err
			},
			code: run.CodeInvalidTransition,
		},
		{
			name: "active blocker",
			setup: func(t *testing.T, f *runFixture, taskValue taskmodel.Task) error {
				blocker := newTask(t, f, taskValue.ProjectID, "blocker")
				_, err := f.tasks.AddDependency(context.Background(), blocker.ID, taskValue.ID, taskValue.Revision, testTaskActor)
				return err
			},
			code: run.CodeConflict,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newRunFixture(t)
			projectValue := newProject(t, f, c.name)
			targetTask := newTask(t, f, projectValue.ID, "target")
			contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "body", runmodel.KindDecision)
			snapshot := newSnapshot(t, projectValue.ID, map[string]string{contextValue.ID: strings.Repeat("a", 64)}, contextValue)

			if err := c.setup(t, f, targetTask); err != nil {
				t.Fatal(err)
			}

			runsBefore := countRows(t, f.db, "SELECT COUNT(*) FROM runs")
			snapshotsBefore := countRows(t, f.db, "SELECT COUNT(*) FROM context_snapshots")
			eventsBefore := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id = ?", targetTask.ID)

			_, err := f.runs.Claim(context.Background(), run.ClaimRecord{
				RunID:      mustRunID(t),
				TaskID:     targetTask.ID,
				EventID:    mustRunID(t),
				Snapshot:   snapshot,
				BaseBranch: "main",
				BaseCommit: "abc123",
				Actor:      testRunActor,
			})
			if run.ErrorCode(err) != c.code {
				t.Fatalf("expected %s, got %v", c.code, err)
			}

			if got := countRows(t, f.db, "SELECT COUNT(*) FROM runs"); got != runsBefore {
				t.Fatalf("runs = %d, want %d", got, runsBefore)
			}
			if got := countRows(t, f.db, "SELECT COUNT(*) FROM context_snapshots"); got != snapshotsBefore {
				t.Fatalf("snapshots = %d, want %d", got, snapshotsBefore)
			}
			if got := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id = ?", targetTask.ID); got != eventsBefore {
				t.Fatalf("events = %d, want %d", got, eventsBefore)
			}
		})
	}
}

func TestRunRepoConcurrentClaimHasOneActiveRun(t *testing.T) {
	database := filepath.Join(t.TempDir(), "istok.db")
	left := newRunFixtureAtPath(t, database, nil)
	right := newRunFixtureAtPath(t, database, nil)

	projectValue := newProject(t, left, "concurrent")
	target := newTask(t, left, projectValue.ID, "concurrent")
	contextValue := newContextRecord(t, left, projectValue.ID, "snapshot", "snapshot", runmodel.KindDecision)
	snapshot := newSnapshot(t, projectValue.ID, nil, contextValue)

	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		fixture := left
		if i == 1 {
			fixture = right
		}
		go func() {
			defer wg.Done()
			<-start
			_, err := fixture.runs.Claim(context.Background(), run.ClaimRecord{
				RunID:      mustRunID(t),
				TaskID:     target.ID,
				EventID:    mustRunID(t),
				Snapshot:   snapshot,
				BaseBranch: "main",
				BaseCommit: "abc123",
				Actor:      testRunActor,
			})
			errCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)

	var wins, conflicts int
	for err := range errCh {
		if err == nil {
			wins++
			continue
		}
		if run.ErrorCode(err) == run.CodeActiveRunExists {
			conflicts++
			continue
		}
		t.Fatalf("unexpected claim error: %v", err)
	}

	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
	if got := countRows(t, left.db, "SELECT COUNT(*) FROM runs WHERE task_id=?", target.ID); got != 1 {
		t.Fatalf("run count = %d", got)
	}
	if got := countRows(t, left.db, "SELECT COUNT(*) FROM context_snapshots"); got != 1 {
		t.Fatalf("snapshot count = %d", got)
	}
	if !hasActiveRun(t, left, target.ID) {
		t.Fatal("expected active run")
	}
}

func TestRunRepoFinishRunEvidencePolicy(t *testing.T) {
	f := newRunFixture(t)
	projectValue := newProject(t, f, "evidence")
	targetTask := newTask(t, f, projectValue.ID, "target")

	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", runmodel.KindDecision)
	runValue := claimRun(t, f, targetTask.ID,
		newSnapshot(t, projectValue.ID, nil, contextValue))
	execution := startExecution(t, f, runValue.ID)
	execution = finishExecution(t, f, execution, run.ExecutionSucceeded)

	if err := func() error {
		_, err := f.runs.FinishRun(context.Background(), run.FinishRunInput{
			RunID:            runValue.ID,
			LeaseID:          runValue.LeaseID,
			EventID:          mustRunID(t),
			ExpectedRevision: runValue.Revision,
			Status:           run.StatusSucceeded,
			ResultSummary:    "needs proof",
			AllowUnvalidated: false,
		}, testRunActor)
		return err
	}(); run.ErrorCode(err) != run.CodeEvidenceRequired {
		t.Fatalf("expected evidence required, got %v", err)
	}
	if hasActiveRun(t, f, targetTask.ID) != true {
		t.Fatal("run should stay active after evidence required")
	}

	runValue = finishRun(t, f, runValue, run.StatusSucceeded, true, "manual override")
	if runValue.ValidationOverride != "manual override" {
		t.Fatalf("override = %q", runValue.ValidationOverride)
	}

	failedTask := newTask(t, f, projectValue.ID, "failed")
	failedRun := claimRun(t, f, failedTask.ID, newSnapshot(t, projectValue.ID, nil,
		newContextRecord(t, f, projectValue.ID, "snapshot-2", "snapshot", runmodel.KindDecision)))
	failedExecution := startExecution(t, f, failedRun.ID)
	failedExecution = finishExecution(t, f, failedExecution, run.ExecutionSucceeded)
	failedRun = finishRun(t, f, failedRun, run.StatusFailed, false, "")
	if failedRun.Status != run.StatusFailed {
		t.Fatalf("failed run status = %s", failedRun.Status)
	}
}

func TestRunRepoFinishRunRequiresRunningExecutionStopAndFinishExecutionCAS(t *testing.T) {
	f := newRunFixture(t)
	projectValue := newProject(t, f, "run-finish")
	targetTask := newTask(t, f, projectValue.ID, "target")
	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", runmodel.KindDecision)
	runValue := claimRun(t, f, targetTask.ID, newSnapshot(t, projectValue.ID, nil, contextValue))
	execution := startExecution(t, f, runValue.ID)

	if _, err := f.runs.FinishRun(context.Background(), run.FinishRunInput{
		RunID:            runValue.ID,
		LeaseID:          runValue.LeaseID,
		EventID:          mustRunID(t),
		ExpectedRevision: runValue.Revision,
		Status:           run.StatusBlocked,
		ResultSummary:    "blocked while running",
		AllowUnvalidated: true,
		OverrideReason:   "allowed",
	}, testRunActor); run.ErrorCode(err) != run.CodeInvalidTransition {
		t.Fatalf("expected invalid transition, got %v", err)
	}

	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		status := run.ExecutionCancelled
		if i == 1 {
			status = run.ExecutionFailed
		}
		exitCode := 2
		go func(status run.ExecutionStatus) {
			defer wg.Done()
			<-start
			_, err := f.runs.FinishExecution(context.Background(), run.FinishExecutionInput{
				ExecutionID:      execution.ID,
				ExpectedRevision: execution.Revision,
				Status:           status,
				ExitCode:         &exitCode,
				DurationMS:       int64Ptr(100),
			}, testRunActor)
			errCh <- err
		}(status)
	}
	close(start)
	wg.Wait()
	close(errCh)

	var success, conflicts int
	for err := range errCh {
		if err == nil {
			success++
			continue
		}
		switch run.ErrorCode(err) {
		case run.CodeRevisionConflict, run.CodeInvalidTransition:
			conflicts++
		default:
			t.Fatalf("unexpected finish execution error: %v", err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}

func TestRunRepoExpiredLeaseRecoveryRevokesOldOwnerToken(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	f := newRunFixtureWithClock(t, func() time.Time { return now })
	projectValue := newProject(t, f, "lease-recovery")
	targetTask := newTask(t, f, projectValue.ID, "target")
	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", runmodel.KindDecision)
	runValue := claimRun(t, f, targetTask.ID, newSnapshot(t, projectValue.ID, nil, contextValue))
	running := startExecution(t, f, runValue.ID)
	oldLease := runValue.LeaseID

	now = now.Add(16 * time.Minute)
	if _, err := f.runs.Heartbeat(context.Background(), run.HeartbeatInput{
		RunID:         runValue.ID,
		LeaseID:       oldLease,
		LeaseDuration: time.Minute,
	}, testRunActor); run.ErrorCode(err) != run.CodeConflict {
		t.Fatalf("expired Heartbeat() error = %v", err)
	}

	newLease := mustRunID(t)
	recovered, err := f.runs.Recover(context.Background(), run.RecoverInput{
		RunID:         runValue.ID,
		LeaseID:       newLease,
		LeaseDuration: 15 * time.Minute,
		Reason:        "previous process stopped heartbeating",
		EventID:       mustRunID(t),
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.LeaseID != newLease || recovered.Revision != runValue.Revision+1 {
		t.Fatalf("recovered run = %#v", recovered)
	}

	cancelled, err := f.runs.GetExecution(context.Background(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != run.ExecutionCancelled || cancelled.Signal != "recovered" {
		t.Fatalf("recovered execution = %#v", cancelled)
	}

	if _, err := f.runs.StartExecution(context.Background(), run.StartExecutionInput{
		ID:      mustRunID(t),
		RunID:   recovered.ID,
		LeaseID: oldLease,
		Argv:    []string{"echo", "stale"},
		CWD:     "/tmp",
	}, testRunActor); run.ErrorCode(err) != run.CodeConflict {
		t.Fatalf("old-lease StartExecution() error = %v", err)
	}

	active := startExecution(t, f, recovered.ID)
	active = finishExecution(t, f, active, run.ExecutionFailed)
	if _, err := f.runs.FinishRun(context.Background(), run.FinishRunInput{
		RunID:            recovered.ID,
		LeaseID:          oldLease,
		EventID:          mustRunID(t),
		ExpectedRevision: recovered.Revision,
		Status:           run.StatusFailed,
		ResultSummary:    "stale owner",
	}, testRunActor); run.ErrorCode(err) != run.CodeConflict {
		t.Fatalf("old-lease FinishRun() error = %v", err)
	}

	finished := finishRun(t, f, recovered, run.StatusFailed, false, "")
	if finished.Status != run.StatusFailed {
		t.Fatalf("finished run = %#v", finished)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id=? AND type='run_recovered'", targetTask.ID); got != 1 {
		t.Fatalf("recovery event count = %d", got)
	}
}

func TestRunRepoManagedFinishIsAtomicAndCreatesExecutedEvidence(t *testing.T) {
	f := newRunFixture(t)
	projectValue := newProject(t, f, "managed-finish")
	targetTask := newTask(t, f, projectValue.ID, "target")
	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", runmodel.KindDecision)
	runValue := claimRun(t, f, targetTask.ID, newSnapshot(t, projectValue.ID, nil, contextValue))
	execution := startExecution(t, f, runValue.ID)
	exitCode := 0
	duration := int64(25)
	validationID := mustRunID(t)

	result, err := f.runs.FinishManagedExecution(context.Background(), run.FinishManagedExecutionInput{
		FinishExecutionInput: run.FinishExecutionInput{
			ExecutionID:      execution.ID,
			ExpectedRevision: execution.Revision,
			Status:           run.ExecutionSucceeded,
			ExitCode:         &exitCode,
			DurationMS:       &duration,
		},
		Artifacts: []run.ArtifactInput{
			managedArtifactInput(t, execution.ID, run.ArtifactKindStdout, strings.Repeat("a", 64)),
			managedArtifactInput(t, execution.ID, run.ArtifactKindStderr, strings.Repeat("b", 64)),
		},
		Validation: &run.RecordValidationInput{
			ID:          validationID,
			ExecutionID: execution.ID,
			Source:      run.ValidationSourceExecuted,
			Status:      run.ValidationStatusPassed,
			Command:     "go test ./...",
			ExitCode:    &exitCode,
			DurationMS:  &duration,
			Summary:     "tests passed",
		},
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Execution.Status != run.ExecutionSucceeded || len(result.Artifacts) != 2 || result.Validation == nil || result.Validation.ID != validationID {
		t.Fatalf("managed result = %#v", result)
	}

	shown, err := f.runs.ShowRun(context.Background(), runValue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shown.Artifacts) != 2 || len(shown.Validations) != 1 {
		t.Fatalf("shown evidence = %#v", shown)
	}

	second := startExecution(t, f, runValue.ID)
	failedExit := 1
	badDuration := int64(10)
	_, err = f.runs.FinishManagedExecution(context.Background(), run.FinishManagedExecutionInput{
		FinishExecutionInput: run.FinishExecutionInput{
			ExecutionID:      second.ID,
			ExpectedRevision: second.Revision,
			Status:           run.ExecutionFailed,
			ExitCode:         &failedExit,
			DurationMS:       &badDuration,
		},
		Artifacts: []run.ArtifactInput{
			managedArtifactInput(t, second.ID, run.ArtifactKindStdout, strings.Repeat("c", 64)),
			managedArtifactInput(t, second.ID, run.ArtifactKindStderr, strings.Repeat("d", 64)),
		},
		Validation: &run.RecordValidationInput{
			ID:          mustRunID(t),
			ExecutionID: second.ID,
			Source:      run.ValidationSourceExecuted,
			Status:      run.ValidationStatusPassed,
			Command:     "false",
			ExitCode:    &failedExit,
			DurationMS:  &badDuration,
			Summary:     "incorrect assertion",
		},
	}, testRunActor)
	if run.ErrorCode(err) != run.CodeInvalid {
		t.Fatalf("mismatched managed validation error = %v", err)
	}
	second, err = f.runs.GetExecution(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != run.ExecutionRunning || second.Revision != 1 {
		t.Fatalf("managed rollback execution = %#v", second)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM artifacts WHERE run_id=?", runValue.ID); got != 2 {
		t.Fatalf("artifact count after rollback = %d", got)
	}
	second = finishExecution(t, f, second, run.ExecutionFailed)

	_, err = f.runs.RecordValidation(context.Background(), run.RecordValidationInput{
		ID:          mustRunID(t),
		ExecutionID: second.ID,
		Source:      run.ValidationSourceExecuted,
		Status:      run.ValidationStatusFailed,
		Command:     "false",
		ExitCode:    &failedExit,
		Summary:     "caller asserted execution",
	}, testRunActor)
	if run.ErrorCode(err) != run.CodeInvalid {
		t.Fatalf("manual executed validation error = %v", err)
	}
}

func managedArtifactInput(t *testing.T, executionID string, kind run.ArtifactKind, hash string) run.ArtifactInput {
	t.Helper()

	return run.ArtifactInput{
		ID:           mustRunID(t),
		ExecutionID:  executionID,
		Kind:         kind,
		RelativePath: "runs/run/executions/execution/" + string(kind) + ".log",
		SHA256:       hash,
		OriginalSize: 3,
		StoredSize:   3,
		MediaType:    "text/plain; charset=utf-8",
	}
}

func TestRunRepoRecordValidationTerminalExecutionRunChecks(t *testing.T) {
	f := newRunFixture(t)
	projectValue := newProject(t, f, "validation")
	taskValue := newTask(t, f, projectValue.ID, "target")

	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", runmodel.KindDecision)
	runValue := claimRun(t, f, taskValue.ID, newSnapshot(t, projectValue.ID, nil, contextValue))
	execution := startExecution(t, f, runValue.ID)

	if _, err := f.runs.RecordValidation(context.Background(), run.RecordValidationInput{
		ExecutionID: execution.ID,
		Source:      run.ValidationSourceAttested,
		Status:      run.ValidationStatusPassed,
		Command:     "echo",
		Summary:     "running",
		ID:          mustRunID(t),
	}, testRunActor); run.ErrorCode(err) != run.CodeInvalidTransition {
		t.Fatalf("running execution validation = %v", err)
	}

	execution = finishExecution(t, f, execution, run.ExecutionSucceeded)
	validation := recordValidation(t, f, execution.ID, run.ValidationStatusPassed)
	if validation.ID == "" {
		t.Fatal("validation was not recorded")
	}
	_ = finishRun(t, f, runValue, run.StatusSucceeded, false, "done")

	if _, err := f.runs.RecordValidation(context.Background(), run.RecordValidationInput{
		ExecutionID: execution.ID,
		Source:      run.ValidationSourceAttested,
		Status:      run.ValidationStatusPassed,
		Command:     "echo again",
		Summary:     "blocked",
		ID:          mustRunID(t),
	}, testRunActor); run.ErrorCode(err) != run.CodeInvalidTransition {
		t.Fatalf("terminal run validation = %v", err)
	}
}

func TestRunRepoCompleteTaskStrictEvidenceAndConcurrentCAS(t *testing.T) {
	f := newRunFixture(t)
	projectValue := newProject(t, f, "complete-evidence")
	targetTask := newTask(t, f, projectValue.ID, "target")
	otherTask := newTask(t, f, projectValue.ID, "other")

	runTarget, _, validationTarget := buildSucceededRunWithValidation(t, f, targetTask, projectValue.ID)
	runOther, _, validationOther := buildSucceededRunWithValidation(t, f, otherTask, projectValue.ID)
	runTarget = finishRun(t, f, runTarget, run.StatusSucceeded, false, "done")
	runOther = finishRun(t, f, runOther, run.StatusSucceeded, false, "done")

	targetTask, err := f.tasks.Get(context.Background(), targetTask.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.runs.CompleteTask(context.Background(), run.CompletionRecord{
		ID:                   mustRunID(t),
		TaskID:               targetTask.ID,
		EventID:              mustRunID(t),
		ExpectedTaskRevision: targetTask.Revision,
		RunID:                &runOther.ID,
		ValidationID:         &validationTarget.ID,
		Note:                 "wrong run",
		Actor:                testRunActor,
		CreatedAt:            time.Unix(1, 0).UTC(),
	}); run.ErrorCode(err) != run.CodeEvidenceRequired {
		t.Fatalf("expected evidence error for mismatched run, got %v", err)
	}

	if _, _, err := f.runs.CompleteTask(context.Background(), run.CompletionRecord{
		ID:                   mustRunID(t),
		TaskID:               targetTask.ID,
		EventID:              mustRunID(t),
		ExpectedTaskRevision: targetTask.Revision,
		RunID:                &runTarget.ID,
		ValidationID:         &validationOther.ID,
		Note:                 "wrong validation",
		Actor:                testRunActor,
		CreatedAt:            time.Unix(2, 0).UTC(),
	}); run.ErrorCode(err) != run.CodeEvidenceRequired {
		t.Fatalf("expected evidence error for mismatched validation, got %v", err)
	}

	if _, _, err := f.runs.CompleteTask(context.Background(), run.CompletionRecord{
		ID:                   mustRunID(t),
		TaskID:               targetTask.ID,
		EventID:              mustRunID(t),
		ExpectedTaskRevision: targetTask.Revision,
		RunID:                nil,
		ValidationID:         &validationOther.ID,
		Note:                 "wrong task",
		Actor:                testRunActor,
		CreatedAt:            time.Unix(3, 0).UTC(),
	}); run.ErrorCode(err) != run.CodeConflict {
		t.Fatalf("expected conflict for foreign validation, got %v", err)
	}

	completedTask, completion := completeTask(t, f, targetTask, nil, nil, "audited override")
	if completedTask.Status != taskmodel.StatusDone {
		t.Fatalf("status after override = %s", completedTask.Status)
	}
	if completion.OverrideReason != "audited override" {
		t.Fatalf("override reason = %q", completion.OverrideReason)
	}
	if completion.RunID != nil || completion.ValidationID != nil {
		t.Fatalf("unexpected evidence fields: %#v", completion)
	}

	concurrentTask := newTask(t, f, projectValue.ID, "parallel")
	concurrentRun, _, concurrentValidation := buildSucceededRunWithValidation(t, f, concurrentTask, projectValue.ID)
	concurrentRun = finishRun(t, f, concurrentRun, run.StatusSucceeded, false, "done")
	current, err := f.tasks.Get(context.Background(), concurrentTask.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := f.runs.CompleteTask(context.Background(), run.CompletionRecord{
				ID:                   mustRunID(t),
				TaskID:               concurrentTask.ID,
				EventID:              mustRunID(t),
				ExpectedTaskRevision: current.Revision,
				RunID:                &concurrentRun.ID,
				ValidationID:         &concurrentValidation.ID,
				Note:                 "parallel complete",
				Actor:                testRunActor,
				CreatedAt:            time.Unix(4, 0).UTC(),
			})
			errCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)

	var done, conflicts int
	for err := range errCh {
		if err == nil {
			done++
			continue
		}
		if run.ErrorCode(err) == run.CodeRevisionConflict || run.ErrorCode(err) == run.CodeInvalidTransition {
			conflicts++
			continue
		}
		t.Fatalf("unexpected complete task error: %v", err)
	}
	if done != 1 || conflicts != 1 {
		t.Fatalf("done=%d conflicts=%d", done, conflicts)
	}
}

func TestRunRepoSnapshotImmutabilityAfterContextMutation(t *testing.T) {
	f := newRunFixture(t)
	projectValue := newProject(t, f, "snapshot")
	targetTask := newTask(t, f, projectValue.ID, "target")

	record := newContextRecord(t, f, projectValue.ID, "decision", "original body", runmodel.KindDecision)
	hash := hashOf("decision", record.ID, record.Title, record.Body, fmt.Sprint(record.Revision))
	runValue := claimRun(t, f, targetTask.ID, newSnapshot(t, projectValue.ID, map[string]string{record.ID: hash}, record))

	updatedTitle := "updated"
	updatedRecord, err := f.contextRecords.Update(context.Background(), record.ID, record.Revision, runmodel.Patch{Title: &updatedTitle}, testContextActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.contextRecords.Archive(context.Background(), record.ID, updatedRecord.Revision, testContextActor); err != nil {
		t.Fatal(err)
	}

	show, err := f.runs.ShowRun(context.Background(), runValue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Snapshot.Records) != 1 {
		t.Fatalf("snapshot records = %d", len(show.Snapshot.Records))
	}
	snapshotItem := show.Snapshot.Records[0]
	if snapshotItem.RecordRevision != record.Revision {
		t.Fatalf("revision = %d, want %d", snapshotItem.RecordRevision, record.Revision)
	}
	if snapshotItem.ContentHash != hash {
		t.Fatalf("hash changed %q -> %q", hash, snapshotItem.ContentHash)
	}
	if snapshotItem.Body != record.Body {
		t.Fatalf("snapshot body changed: %q", snapshotItem.Body)
	}
}

func TestRunRepoHasActiveRunAndDerivedTaskState(t *testing.T) {
	f := newRunFixture(t)
	service := taskapp.NewService(f.tasks, f.runs)
	projectValue := newProject(t, f, "derived")

	activeTask := newTask(t, f, projectValue.ID, "active")
	idleTask := newTask(t, f, projectValue.ID, "idle")

	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", runmodel.KindDecision)
	_, err := f.runs.Claim(context.Background(), run.ClaimRecord{
		RunID:      mustRunID(t),
		TaskID:     activeTask.ID,
		EventID:    mustRunID(t),
		Snapshot:   newSnapshot(t, projectValue.ID, nil, contextValue),
		BaseBranch: "main",
		BaseCommit: "abc123",
		Actor:      testRunActor,
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := service.List(context.Background(), projectValue.ID, taskmodel.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list size = %d", len(list))
	}
	for _, item := range list {
		switch item.ID {
		case activeTask.ID:
			if !item.HasActiveRun {
				t.Fatalf("active task %s has no active run", item.ID)
			}
		case idleTask.ID:
			if item.HasActiveRun {
				t.Fatalf("idle task %s has active run", item.ID)
			}
		default:
			t.Fatalf("unexpected task id in list: %s", item.ID)
		}
	}

	activeShow, err := service.Show(context.Background(), taskmodel.Selector{ProjectID: projectValue.ID, ID: activeTask.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	idleShow, err := service.Show(context.Background(), taskmodel.Selector{ProjectID: projectValue.ID, ID: idleTask.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !activeShow.HasActiveRun {
		t.Fatal("active show should have active run")
	}
	if idleShow.HasActiveRun {
		t.Fatal("idle show should not have active run")
	}
	if !hasActiveRun(t, f, activeTask.ID) || hasActiveRun(t, f, idleTask.ID) {
		t.Fatal("HasActiveRun returned unexpected value")
	}
}

func TestRunRepoListRunsWithFiltersAndDeterministicOrdering(t *testing.T) {
	fixture := newRunFixtureWithClock(t, newClock(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), time.Minute))
	projectOne := newProject(t, fixture, "p1")
	projectTwo := newProject(t, fixture, "p2")

	taskOne := newTask(t, fixture, projectOne.ID, "one")
	taskTwo := newTask(t, fixture, projectOne.ID, "two")
	taskOther := newTask(t, fixture, projectTwo.ID, "other")

	recordOne := newContextRecord(t, fixture, projectOne.ID, "r1", "body", runmodel.KindDecision)
	recordTwo := newContextRecord(t, fixture, projectOne.ID, "r2", "body", runmodel.KindDecision)
	recordThree := newContextRecord(t, fixture, projectOne.ID, "r3", "body", runmodel.KindDecision)
	recordFour := newContextRecord(t, fixture, projectTwo.ID, "r4", "body", runmodel.KindDecision)

	runOne := claimRun(t, fixture, taskOne.ID, newSnapshot(t, projectOne.ID, nil, recordOne))
	execOne := startExecution(t, fixture, runOne.ID)
	execOne = finishExecution(t, fixture, execOne, run.ExecutionSucceeded)
	_ = recordValidation(t, fixture, execOne.ID, run.ValidationStatusPassed)
	runOne = finishRun(t, fixture, runOne, run.StatusSucceeded, false, "done")

	runTwo := claimRun(t, fixture, taskTwo.ID, newSnapshot(t, projectOne.ID, nil, recordTwo))
	execTwo := startExecution(t, fixture, runTwo.ID)
	execTwo = finishExecution(t, fixture, execTwo, run.ExecutionSucceeded)
	runTwo = finishRun(t, fixture, runTwo, run.StatusFailed, false, "failed")

	runThree := claimRun(t, fixture, taskTwo.ID, newSnapshot(t, projectOne.ID, nil, recordThree))

	_, _ = buildCompletedRunWithPassedValidation(t, fixture, taskOther, projectTwo.ID, recordFour.ID, recordFour.Body)

	allRuns, err := fixture.runs.ListRuns(context.Background(), run.ListOptions{ProjectID: projectOne.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(allRuns) != 3 {
		t.Fatalf("project run count = %d", len(allRuns))
	}
	if got := []string{allRuns[0].ID, allRuns[1].ID, allRuns[2].ID}; got[0] != runThree.ID || got[1] != runTwo.ID || got[2] != runOne.ID {
		t.Fatalf("unexpected project order: %#v", got)
	}

	taskRuns, err := fixture.runs.ListRuns(context.Background(), run.ListOptions{TaskID: taskTwo.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(taskRuns) != 2 {
		t.Fatalf("task two runs = %d", len(taskRuns))
	}
	if taskRuns[0].ID != runThree.ID || taskRuns[1].ID != runTwo.ID {
		t.Fatalf("unexpected task two order: %#v", taskRuns)
	}

	failed, err := fixture.runs.ListRuns(context.Background(), run.ListOptions{ProjectID: projectOne.ID, Statuses: []run.Status{run.StatusFailed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].ID != runTwo.ID {
		t.Fatalf("failed runs = %#v", failed)
	}

	limited, err := fixture.runs.ListRuns(context.Background(), run.ListOptions{ProjectID: projectOne.ID, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Fatalf("limited count = %d", len(limited))
	}
	if limited[0].ID != runThree.ID || limited[1].ID != runTwo.ID {
		t.Fatalf("limited order = %#v", limited)
	}
}

func TestRunRepoTransactionRollbackOnDuplicateIDsAndInvalidEvidence(t *testing.T) {
	f := newRunFixture(t)
	projectValue := newProject(t, f, "rollback")
	targetTask := newTask(t, f, projectValue.ID, "target")
	otherTask := newTask(t, f, projectValue.ID, "other")

	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "target", runmodel.KindDecision)
	snapshot := newSnapshot(t, projectValue.ID, nil, contextValue)
	otherContext := newContextRecord(t, f, projectValue.ID, "snapshot2", "other", runmodel.KindDecision)
	otherSnapshot := newSnapshot(t, projectValue.ID, nil, otherContext)

	runID := mustRunID(t)
	runsBefore := countRows(t, f.db, "SELECT COUNT(*) FROM runs")
	snapshotsBefore := countRows(t, f.db, "SELECT COUNT(*) FROM context_snapshots")

	if _, err := f.runs.Claim(context.Background(), run.ClaimRecord{
		RunID:      runID,
		TaskID:     targetTask.ID,
		EventID:    mustRunID(t),
		Snapshot:   snapshot,
		BaseBranch: "main",
		BaseCommit: "abc123",
		Actor:      testRunActor,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := f.runs.Claim(context.Background(), run.ClaimRecord{
		RunID:      runID,
		TaskID:     otherTask.ID,
		EventID:    mustRunID(t),
		Snapshot:   otherSnapshot,
		BaseBranch: "main",
		BaseCommit: "abc123",
		Actor:      testRunActor,
	})
	if run.ErrorCode(err) != run.CodeConflict {
		t.Fatalf("expected claim duplicate conflict, got %v", err)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM runs"); got != runsBefore+1 {
		t.Fatalf("runs = %d want %d", got, runsBefore+1)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM context_snapshots"); got != snapshotsBefore+1 {
		t.Fatalf("snapshots = %d want %d", got, snapshotsBefore+1)
	}

	otherRun, _, otherValidation := buildSucceededRunWithValidation(
		t,
		f,
		otherTask,
		projectValue.ID,
	)
	otherRun = finishRun(t, f, otherRun, run.StatusSucceeded, false, "")

	targetBeforeCompletions := countRows(t, f.db, "SELECT COUNT(*) FROM task_completions WHERE task_id=?", targetTask.ID)
	targetBeforeEvents := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id=? AND type='completed'", targetTask.ID)

	_, _, err = f.runs.CompleteTask(context.Background(), run.CompletionRecord{
		ID:                   mustRunID(t),
		TaskID:               targetTask.ID,
		EventID:              mustRunID(t),
		ExpectedTaskRevision: targetTask.Revision,
		RunID:                &otherRun.ID,
		ValidationID:         &otherValidation.ID,
		Note:                 "invalid evidence",
		Actor:                testRunActor,
		CreatedAt:            time.Unix(5, 0).UTC(),
	})
	if run.ErrorCode(err) != run.CodeConflict {
		t.Fatalf("expected invalid evidence conflict, got %v", err)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM task_completions WHERE task_id=?", targetTask.ID); got != targetBeforeCompletions {
		t.Fatalf("completions = %d want %d", got, targetBeforeCompletions)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id=? AND type='completed'", targetTask.ID); got != targetBeforeEvents {
		t.Fatalf("completed events = %d want %d", got, targetBeforeEvents)
	}
}

func buildSucceededRunWithValidation(
	t *testing.T,
	f *runFixture,
	taskValue taskmodel.Task,
	projectID string,
) (run.Run, run.Execution, run.Validation) {
	t.Helper()

	record := newContextRecord(t, f, projectID, "record", "validation", runmodel.KindDecision)
	runValue := claimRun(t, f, taskValue.ID, newSnapshot(t, projectID, nil, record))
	execution := startExecution(t, f, runValue.ID)
	execution = finishExecution(t, f, execution, run.ExecutionSucceeded)
	validation := recordValidation(t, f, execution.ID, run.ValidationStatusPassed)

	return runValue, execution, validation
}

func buildCompletedRunWithPassedValidation(
	t *testing.T,
	f *runFixture,
	taskValue taskmodel.Task,
	projectID string,
	title string,
	body string,
) (run.Run, run.Validation) {
	t.Helper()

	value, _, validation := buildSucceededRunWithValidation(
		t,
		f,
		taskValue,
		projectID,
	)
	value = finishRun(t, f, value, run.StatusSucceeded, false, "done")
	_ = title
	_ = body
	return value, validation
}

package uireadrepo_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	contextpackapp "github.com/vtimame/istok.sh/internal/application/contextpack"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/storage/runrepo"
	"github.com/vtimame/istok.sh/internal/storage/taskrepo"
	"github.com/vtimame/istok.sh/internal/storage/uireadrepo"
	"github.com/vtimame/istok.sh/internal/task"
)

var (
	testTaskActor = task.ActorSnapshot{ID: "local-user", Kind: "user", Name: "Local User"}
	testRunActor  = run.ActorSnapshot{ID: "local-agent", Kind: "agent", Name: "Local Agent"}
)

// fixture wires the same kernel services as the web UI over a temporary
// database. pastRuns and futureRuns claim through repositories with shifted
// clocks, so their leases are already expired or end well after the test.
type fixture struct {
	db         *sql.DB
	readModel  *uireadrepo.Repository
	projects   *project.Service
	tasks      *taskapp.Service
	runs       *runapp.Service
	pastRuns   *runapp.Service
	futureRuns *runapp.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	t.Setenv("ISTOK_INDEX_ROOT", t.TempDir())
	database := filepath.Join(t.TempDir(), "istok.db")

	f := &fixture{}
	var taskRepository *taskrepo.Repository
	var contextBuilder *contextpackapp.Service

	app := fx.New(
		fx.NopLogger,
		bootstrap.TaskOptions(database),
		fx.Provide(uireadrepo.New),
		fx.Invoke(func(
			db *sql.DB,
			readModel *uireadrepo.Repository,
			projects *project.Service,
			tasks *taskapp.Service,
			runs *runapp.Service,
			tasksRepo *taskrepo.Repository,
			builder *contextpackapp.Service,
		) {
			f.db = db
			f.readModel = readModel
			f.projects = projects
			f.tasks = tasks
			f.runs = runs
			taskRepository = tasksRepo
			contextBuilder = builder
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

	shifted := func(offset time.Duration) *runapp.Service {
		clock := func() time.Time { return time.Now().Add(offset) }
		return runapp.NewService(runrepo.NewWithClock(f.db, clock), taskRepository, contextBuilder)
	}
	f.pastRuns = shifted(-time.Hour)
	f.futureRuns = shifted(time.Hour)

	return f
}

func (f *fixture) newProject(t *testing.T, name string) project.Project {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := f.projects.Init(context.Background(), resolved, name)
	if err != nil {
		t.Fatal(err)
	}

	return result.Project
}

func (f *fixture) newTask(t *testing.T, projectID, title string) task.Task {
	t.Helper()

	value, err := f.tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: title}, testTaskActor)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func (f *fixture) showTask(t *testing.T, value task.Task, deleted bool) task.Task {
	t.Helper()

	shown, err := f.tasks.Show(context.Background(), task.Selector{ProjectID: value.ProjectID, ID: value.ID}, deleted)
	if err != nil {
		t.Fatal(err)
	}

	return shown.Task
}

func claim(t *testing.T, service *runapp.Service, value task.Task) run.Run {
	t.Helper()

	claimed, err := service.Claim(context.Background(), task.Selector{ProjectID: value.ProjectID, ID: value.ID}, run.ClaimInput{
		WithoutRetrieval:        true,
		RetrievalOverrideReason: "test without index",
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return claimed
}

func (f *fixture) abandon(t *testing.T, value run.Run) run.Run {
	t.Helper()

	abandoned, err := f.runs.Abandon(context.Background(), run.AbandonInput{
		RunID:            value.ID,
		ExpectedRevision: value.Revision,
		Reason:           "test cleanup",
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return abandoned
}

func (f *fixture) getRun(t *testing.T, id string) run.Run {
	t.Helper()

	value, err := f.runs.GetRun(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func (f *fixture) recordValidations(t *testing.T, value run.Run, statuses ...run.ValidationStatus) {
	t.Helper()
	ctx := context.Background()

	for _, status := range statuses {
		execution, err := f.runs.StartExecution(ctx, run.StartExecutionInput{
			RunID:   value.ID,
			LeaseID: value.LeaseID,
			Argv:    []string{"go", "test"},
			CWD:     "/tmp",
		}, testRunActor)
		if err != nil {
			t.Fatal(err)
		}

		exitCode := 0
		execution, err = f.runs.FinishExecution(ctx, run.FinishExecutionInput{
			ExecutionID:      execution.ID,
			ExpectedRevision: execution.Revision,
			Status:           run.ExecutionSucceeded,
			ExitCode:         &exitCode,
		}, testRunActor)
		if err != nil {
			t.Fatal(err)
		}

		_, err = f.runs.RecordValidation(ctx, run.RecordValidationInput{
			ExecutionID: execution.ID,
			Source:      run.ValidationSourceAttested,
			Status:      status,
			Command:     "go test ./...",
			ExitCode:    &exitCode,
			Summary:     string(status),
		}, testRunActor)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func latest(values ...time.Time) time.Time {
	result := values[0]
	for _, value := range values[1:] {
		if value.After(result) {
			result = value
		}
	}

	return result
}

func TestProjectStatsCountsLiveTasksAndRunLeases(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	busy := f.newProject(t, "busy")
	liveTask := f.newTask(t, busy.ID, "live run")
	staleTask := f.newTask(t, busy.ID, "stale run")
	blockedTask := f.newTask(t, busy.ID, "blocked")
	doneTask := f.newTask(t, busy.ID, "done")
	deletedTask := f.newTask(t, busy.ID, "deleted")

	liveRun := claim(t, f.runs, liveTask)
	staleRun := claim(t, f.pastRuns, staleTask)

	if _, err := f.tasks.Block(ctx, task.Selector{ProjectID: busy.ID, ID: blockedTask.ID}, blockedTask.Revision, "waiting", testTaskActor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.runs.CompleteTask(ctx, task.Selector{ProjectID: busy.ID, ID: doneTask.ID}, run.CompleteTaskInput{
		ExpectedTaskRevision: doneTask.Revision,
		Note:                 "done",
		OverrideReason:       "test without evidence",
	}, testRunActor); err != nil {
		t.Fatal(err)
	}

	quiet := f.newProject(t, "quiet")
	quietTask := f.newTask(t, quiet.ID, "finished run")
	finishedRun := f.abandon(t, claim(t, f.runs, quietTask))

	heartbeat := f.newProject(t, "heartbeat")
	heartbeatTask := f.newTask(t, heartbeat.ID, "future run")
	futureRun := claim(t, f.futureRuns, heartbeatTask)

	empty := f.newProject(t, "empty")

	// Deleted last, so its updated_at is the newest task time in the database
	// and would win the activity maximum if deleted tasks were not excluded.
	if _, err := f.tasks.Delete(ctx, task.Selector{ProjectID: busy.ID, ID: deletedTask.ID}, deletedTask.Revision, testTaskActor); err != nil {
		t.Fatal(err)
	}

	stats, err := f.readModel.ProjectStats(ctx)
	if err != nil {
		t.Fatal(err)
	}

	got := stats[busy.ID]
	if got.Open != 2 || got.Blocked != 1 || got.Done != 1 || got.ActiveRuns != 1 || got.StaleRuns != 1 {
		t.Fatalf("busy stats = %+v, want open=2 blocked=1 done=1 active=1 stale=1", got)
	}
	wantBusy := latest(
		f.showTask(t, liveTask, false).UpdatedAt,
		f.showTask(t, staleTask, false).UpdatedAt,
		f.showTask(t, blockedTask, false).UpdatedAt,
		f.showTask(t, doneTask, false).UpdatedAt,
		f.getRun(t, liveRun.ID).UpdatedAt,
		f.getRun(t, staleRun.ID).UpdatedAt,
	)
	if got.LastActivityAt == nil || !got.LastActivityAt.Equal(wantBusy) {
		t.Fatalf("busy last activity = %v, want %v", got.LastActivityAt, wantBusy)
	}
	if deletedAt := f.showTask(t, deletedTask, true).UpdatedAt; !deletedAt.After(wantBusy) {
		t.Fatalf("deleted task updated_at %v is not newer than live activity %v; fixture does not prove exclusion", deletedAt, wantBusy)
	}

	got = stats[quiet.ID]
	if got.Open != 1 || got.Blocked != 0 || got.Done != 0 || got.ActiveRuns != 0 || got.StaleRuns != 0 {
		t.Fatalf("quiet stats = %+v, want only one open task and no active runs", got)
	}
	if got.LastActivityAt == nil || !got.LastActivityAt.Equal(latest(f.showTask(t, quietTask, false).UpdatedAt, finishedRun.UpdatedAt)) {
		t.Fatalf("quiet last activity = %v", got.LastActivityAt)
	}

	got = stats[heartbeat.ID]
	if got.ActiveRuns != 1 || got.StaleRuns != 0 {
		t.Fatalf("heartbeat stats = %+v, want one live run", got)
	}
	if got.LastActivityAt == nil || !got.LastActivityAt.Equal(futureRun.UpdatedAt) {
		t.Fatalf("heartbeat last activity = %v, want run update %v", got.LastActivityAt, futureRun.UpdatedAt)
	}

	if value, found := stats[empty.ID]; found {
		t.Fatalf("project without tasks has stats %+v, want none", value)
	}
}

func TestRunFeedJoinsFiltersAndPaginates(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	alpha := f.newProject(t, "alpha")
	beta := f.newProject(t, "beta")
	gone := f.newProject(t, "gone")

	staleTask := f.newTask(t, alpha.ID, "stale task")
	stale := claim(t, f.pastRuns, staleTask)

	validatedTask := f.newTask(t, alpha.ID, "validated task")
	validated := claim(t, f.runs, validatedTask)
	f.recordValidations(t, validated, run.ValidationStatusPassed, run.ValidationStatusFailed, run.ValidationStatusPassed)
	validated = f.abandon(t, f.getRun(t, validated.ID))

	liveTask := f.newTask(t, beta.ID, "live task")
	live := claim(t, f.runs, liveTask)

	deletedTask := f.newTask(t, beta.ID, "deleted task")
	f.abandon(t, claim(t, f.runs, deletedTask))
	deletedTask = f.showTask(t, deletedTask, false)
	if _, err := f.tasks.Delete(ctx, task.Selector{ProjectID: beta.ID, ID: deletedTask.ID}, deletedTask.Revision, testTaskActor); err != nil {
		t.Fatal(err)
	}

	goneTask := f.newTask(t, gone.ID, "gone task")
	f.abandon(t, claim(t, f.runs, goneTask))
	if _, err := f.projects.Delete(ctx, gone.ID, &gone.Revision); err != nil {
		t.Fatal(err)
	}

	ids := func(options uireadrepo.FeedOptions) []string {
		t.Helper()

		items, err := f.readModel.RunFeed(ctx, options)
		if err != nil {
			t.Fatal(err)
		}

		result := make([]string, 0, len(items))
		for _, item := range items {
			result = append(result, item.Run.ID)
		}
		return result
	}

	cases := []struct {
		name    string
		options uireadrepo.FeedOptions
		want    []string
	}{
		{"all newest first", uireadrepo.FeedOptions{Limit: 50}, []string{live.ID, validated.ID, stale.ID}},
		{"one project", uireadrepo.FeedOptions{ProjectIDs: []string{alpha.ID}, Limit: 50}, []string{validated.ID, stale.ID}},
		{"several projects", uireadrepo.FeedOptions{ProjectIDs: []string{alpha.ID, beta.ID}, Limit: 50}, []string{live.ID, validated.ID, stale.ID}},
		{"deleted project filter", uireadrepo.FeedOptions{ProjectIDs: []string{gone.ID}, Limit: 50}, []string{}},
		{"status abandoned", uireadrepo.FeedOptions{Statuses: []string{"abandoned"}, Limit: 50}, []string{validated.ID}},
		{"status active", uireadrepo.FeedOptions{Statuses: []string{"active"}, Limit: 50}, []string{live.ID, stale.ID}},
		{"several statuses", uireadrepo.FeedOptions{Statuses: []string{"active", "abandoned"}, Limit: 50}, []string{live.ID, validated.ID, stale.ID}},
		{"lease live", uireadrepo.FeedOptions{Lease: uireadrepo.LeaseLive, Limit: 50}, []string{live.ID}},
		{"lease expired", uireadrepo.FeedOptions{Lease: uireadrepo.LeaseExpired, Limit: 50}, []string{stale.ID}},
		{"lease implies active", uireadrepo.FeedOptions{Statuses: []string{"abandoned"}, Lease: uireadrepo.LeaseExpired, Limit: 50}, []string{}},
		{"lease with project", uireadrepo.FeedOptions{ProjectIDs: []string{beta.ID}, Lease: uireadrepo.LeaseExpired, Limit: 50}, []string{}},
		{"limit", uireadrepo.FeedOptions{Limit: 2}, []string{live.ID, validated.ID}},
		{"offset", uireadrepo.FeedOptions{Limit: 1, Offset: 1}, []string{validated.ID}},
		{"offset past end", uireadrepo.FeedOptions{Limit: 10, Offset: 3}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(tc.options)
			if len(got) != len(tc.want) {
				t.Fatalf("run ids = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("run ids = %v, want %v", got, tc.want)
				}
			}
		})
	}

	items, err := f.readModel.RunFeed(ctx, uireadrepo.FeedOptions{ProjectIDs: []string{alpha.ID}, Statuses: []string{"abandoned"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("feed items = %+v, want the validated run", items)
	}

	item := items[0]
	if item.Run.Revision != validated.Revision || item.Run.Status != "abandoned" || item.Run.ActorName != testRunActor.Name || item.Run.ActorKind != testRunActor.Kind || item.Run.ResultSummary != "test cleanup" {
		t.Fatalf("feed run = %+v, want %+v", item.Run, validated)
	}
	if !item.Run.StartedAt.Equal(validated.StartedAt) || item.Run.FinishedAt == nil || item.Run.HeartbeatAt == nil || item.Run.ExpiresAt == nil {
		t.Fatalf("feed run times = %+v", item.Run)
	}
	if item.Task.ID != validatedTask.ID || item.Task.Number != validatedTask.Number || item.Task.Title != "validated task" || item.Task.Status != string(task.StatusOpen) {
		t.Fatalf("feed task = %+v, want %+v", item.Task, validatedTask)
	}
	if item.Project.ID != alpha.ID || item.Project.Name != "alpha" {
		t.Fatalf("feed project = %+v", item.Project)
	}
	if item.Validations.Passed != 2 || item.Validations.Failed != 1 {
		t.Fatalf("feed validations = %+v, want passed=2 failed=1", item.Validations)
	}

	items, err = f.readModel.RunFeed(ctx, uireadrepo.FeedOptions{Lease: uireadrepo.LeaseExpired, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Run.FinishedAt != nil || items[0].Run.ExpiresAt == nil || !items[0].Run.ExpiresAt.Before(time.Now()) {
		t.Fatalf("expired feed = %+v, want one unfinished run with a past lease", items)
	}
	if items[0].Validations.Passed != 0 || items[0].Validations.Failed != 0 {
		t.Fatalf("stale run validations = %+v, want none", items[0].Validations)
	}
}

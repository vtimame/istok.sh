package cli

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	"github.com/vtimame/istok.sh/internal/storage/taskrepo"
	"github.com/vtimame/istok.sh/internal/task"
)

func TestTaskListUsesCurrentProjectFromCWDAndExcludesDoneOrDeleted(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	database := filepath.Join(t.TempDir(), "istok.db")

	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID

	repo, db := newTaskRepositoryForTest(t, database)
	ctx := context.Background()
	actor := task.ActorSnapshot{ID: "cli", Kind: "cli", Name: "CLI"}

	ready, err := repo.Create(ctx, task.CreateInput{ProjectID: projectID, Title: "ready task"}, actor)
	if err != nil {
		t.Fatalf("create ready task: %v", err)
	}
	blocker, err := repo.Create(ctx, task.CreateInput{ProjectID: projectID, Title: "blocker"}, actor)
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	blocked, err := repo.Create(ctx, task.CreateInput{ProjectID: projectID, Title: "blocked by blocker"}, actor)
	if err != nil {
		t.Fatalf("create blocked task: %v", err)
	}
	if _, err := repo.AddDependency(ctx, blocker.ID, blocked.ID, blocked.Revision, actor); err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	explicitBlocked, err := repo.Create(ctx, task.CreateInput{ProjectID: projectID, Title: "explicitly blocked"}, actor)
	if err != nil {
		t.Fatalf("create explicit blocked task: %v", err)
	}
	if _, err := repo.Block(ctx, explicitBlocked.ID, explicitBlocked.Revision, "manual block", actor); err != nil {
		t.Fatalf("block task: %v", err)
	}

	done, err := repo.Create(ctx, task.CreateInput{ProjectID: projectID, Title: "done task"}, actor)
	if err != nil {
		t.Fatalf("create done task: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE tasks SET status='done' WHERE id=?", done.ID); err != nil {
		t.Fatalf("mark done task: %v", err)
	}

	deleted, err := repo.Create(ctx, task.CreateInput{ProjectID: projectID, Title: "deleted task"}, actor)
	if err != nil {
		t.Fatalf("create deleted task: %v", err)
	}
	if _, err := repo.Delete(ctx, deleted.ID, deleted.Revision, actor); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	result := executeAt(t, nested, database, "task", "list")
	if result.err != nil {
		t.Fatalf("task list: %v", result.err)
	}

	output := result.output
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("task list output is not ANSI-colored:\n%q", output)
	}
	plain := stripANSI(output)
	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("task list output has no trailing newline:\n%s", output)
	}
	lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("task list output unexpectedly short:\n%s", output)
	}
	if !strings.Contains(plain, filepath.Base(root)+" │") {
		t.Fatalf("task list output missing project name and divider:\n%s", output)
	}
	if !strings.Contains(plain, "4 open tasks") {
		t.Fatalf("task list output missing open tasks count:\n%s", output)
	}
	if !strings.Contains(plain, "#") || !strings.Contains(plain, "STATE") || !strings.Contains(plain, "TITLE") {
		t.Fatalf("task list output is not StyleLight table:\n%s", output)
	}
	if !strings.Contains(plain, "BLOCKED BY") {
		t.Fatalf("task list output missing blocked-by header:\n%s", output)
	}
	for _, unexpected := range []string{done.Title, deleted.Title} {
		if strings.Contains(plain, unexpected) {
			t.Fatalf("task list output unexpectedly contains %q:\n%s", unexpected, output)
		}
	}

	for _, want := range []string{
		ready.Title,
		blocker.Title,
		blocked.Title,
		explicitBlocked.Title,
		"READY",
		"BLOCKED",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("task list output missing %q:\n%s", want, output)
		}
	}

	readyIndex := strings.Index(plain, ready.Title)
	blockerIndex := strings.Index(plain, blocker.Title)
	blockedIndex := strings.Index(plain, blocked.Title)
	explicitIndex := strings.Index(plain, explicitBlocked.Title)
	if readyIndex >= blockerIndex || blockerIndex >= blockedIndex || blockedIndex >= explicitIndex {
		t.Fatalf("task list order incorrect:\n%s", output)
	}
	readyRow := rowForTitle(plain, ready.Title)
	if !strings.Contains(readyRow, "READY") {
		t.Fatalf("ready row is not READY:\n%q", readyRow)
	}
	blockedRow := rowForTitle(plain, blocked.Title)
	if !strings.Contains(blockedRow, "BLOCKED") {
		t.Fatalf("blocked-by row is not BLOCKED:\n%q", blockedRow)
	}
	explicitRow := rowForTitle(plain, explicitBlocked.Title)
	if !strings.Contains(explicitRow, "BLOCKED") {
		t.Fatalf("explicitly blocked row is not BLOCKED:\n%q", explicitRow)
	}
	if !strings.Contains(plain, "#"+strconv.FormatInt(blocker.Number, 10)) {
		t.Fatalf("blocked task blocked-by column is missing blocker #%d: \n%s", blocker.Number, output)
	}
	if strings.ContainsAny(plain, "┌┐└┘") {
		t.Fatalf("task list output still uses boxed table borders:\n%s", output)
	}
}

func TestEmptyTaskListOutputsExactNoOpenTasksMessage(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")

	result := executeAt(t, root, database, "task", "list")
	if result.err != nil {
		t.Fatalf("empty task list: %v", result.err)
	}
	plain := stripANSI(result.output)
	if !strings.HasSuffix(result.output, "\n") {
		t.Fatalf("task list output does not end with newline:\n%s", result.output)
	}
	if !strings.Contains(result.output, "\x1b[") {
		t.Fatalf("task list output is not ANSI-colored:\n%q", result.output)
	}
	if !strings.Contains(plain, filepath.Base(root)+" │") {
		t.Fatalf("empty task list missing project identity:\n%s", result.output)
	}
	if !strings.Contains(plain, "no open tasks") {
		t.Fatalf("empty task list output = %q", result.output)
	}
}

func TestTaskShowCommandOutputsDetailedTask(t *testing.T) {
	root := t.TempDir()
	otherRoot := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	database := filepath.Join(t.TempDir(), "istok.db")

	initializedRoot := executeAt(t, root, database, "init", "--json")
	initializedOther := executeAt(t, otherRoot, database, "init", "--json")

	rootProjectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initializedRoot.output).Project.ID
	otherProjectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initializedOther.output).Project.ID
	if rootProjectID == otherProjectID {
		t.Fatalf("separate roots resolved to the same project %q", rootProjectID)
	}

	repository, _ := newTaskRepositoryForTest(t, database)
	actor := task.ActorSnapshot{ID: "local-user", Kind: "user", Name: "Local User"}
	ctx := context.Background()

	excluded, err := repository.Create(ctx, task.CreateInput{ProjectID: otherProjectID, Title: "excluded other-project task"}, actor)
	if err != nil {
		t.Fatalf("create other-project task: %v", err)
	}
	target, err := repository.Create(ctx, task.CreateInput{
		ProjectID:          rootProjectID,
		Title:              "current target",
		Description:        "target details",
		AcceptanceCriteria: "target acceptance",
		Notes:              "target notes",
	}, actor)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	blocker, err := repository.Create(ctx, task.CreateInput{ProjectID: rootProjectID, Title: "current blocker"}, actor)
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	dependent, err := repository.Create(ctx, task.CreateInput{ProjectID: rootProjectID, Title: "current dependent"}, actor)
	if err != nil {
		t.Fatalf("create dependent: %v", err)
	}
	target, err = repository.AddDependency(ctx, blocker.ID, target.ID, target.Revision, actor)
	if err != nil {
		t.Fatalf("add blocker dependency: %v", err)
	}
	_, err = repository.AddDependency(ctx, target.ID, dependent.ID, dependent.Revision, actor)
	if err != nil {
		t.Fatalf("add dependent dependency: %v", err)
	}
	target, err = repository.Comment(ctx, target.ID, target.Revision, "progress updated", actor)
	if err != nil {
		t.Fatalf("comment target: %v", err)
	}

	eventsBefore, err := repository.Events(ctx, target.ID)
	if err != nil {
		t.Fatalf("list target events before show: %v", err)
	}

	result := executeAt(t, nested, database, "task", "show", strconv.FormatInt(target.Number, 10))
	if result.err != nil {
		t.Fatalf("task show: %v", result.err)
	}
	plain := stripANSI(result.output)

	if target.Number != 1 || excluded.Number != 1 {
		t.Fatalf("project-scoped numbers = target #%d, excluded #%d; want both #1", target.Number, excluded.Number)
	}
	if !strings.Contains(plain, filepath.Base(root)+" · #1") {
		t.Fatalf("show output missing task header:\n%s", result.output)
	}
	if !strings.Contains(plain, "State:") || !strings.Contains(plain, "BLOCKED") {
		t.Fatalf("show output missing state:\n%s", result.output)
	}
	expectedRevision := "r" + strconv.FormatInt(target.Revision, 10)
	if !strings.Contains(plain, "Revision:") || !strings.Contains(plain, expectedRevision) {
		t.Fatalf("show output missing revision %q:\n%s", expectedRevision, result.output)
	}
	for _, want := range []string{
		"current target",
		"Description", "target details",
		"Acceptance criteria", "target acceptance",
		"Notes", "target notes",
		"Relations", "Blocked by", "OPEN", "current blocker", "Blocks", "OPEN", "current dependent",
		"History", "commented", "progress updated",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("show output missing %q:\n%s", want, result.output)
		}
	}
	if strings.Contains(plain, excluded.Title) {
		t.Fatalf("show output leaked excluded project task:\n%s", result.output)
	}
	if strings.Contains(plain, target.ID) {
		t.Fatalf("show output exposes target UUID as human identity:\n%s", result.output)
	}
	if !strings.Contains(plain, "#"+strconv.FormatInt(blocker.Number, 10)) {
		t.Fatalf("show output missing blocker #%d:\n%s", blocker.Number, result.output)
	}
	if !strings.Contains(plain, "#"+strconv.FormatInt(dependent.Number, 10)) {
		t.Fatalf("show output missing dependent #%d:\n%s", dependent.Number, result.output)
	}
	commentIndex := strings.Index(plain, "commented")
	updatedIndex := strings.Index(plain, "progress updated")
	if commentIndex == -1 || updatedIndex == -1 || commentIndex > updatedIndex {
		t.Fatalf("event rows not in expected order: %q", plain)
	}
	if !strings.HasSuffix(result.output, "\n") {
		t.Fatalf("show output does not end with newline:\n%s", result.output)
	}
	if !strings.Contains(result.output, "\x1b[") {
		t.Fatalf("show output is not ANSI-colored:\n%q", result.output)
	}

	refreshed, err := repository.Get(ctx, target.ID, false)
	if err != nil {
		t.Fatalf("Get(target) error = %v", err)
	}
	if refreshed.Revision != target.Revision {
		t.Fatalf("show command mutated task: got revision %d, want %d", refreshed.Revision, target.Revision)
	}
	eventsAfter, err := repository.Events(ctx, target.ID)
	if err != nil {
		t.Fatalf("list target events after show: %v", err)
	}
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("show command mutated event history: before=%d after=%d", len(eventsBefore), len(eventsAfter))
	}
}

func TestTaskShowCommandNotFoundForMissingAndDeletedTasks(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")

	result := executeAt(t, root, database, "task", "show", "1")
	if task.ErrorCode(result.err) != task.CodeNotFound {
		t.Fatalf("show missing task error = %v", result.err)
	}

	repository, _ := newTaskRepositoryForTest(t, database)
	ctx := context.Background()
	actor := task.ActorSnapshot{ID: "local-user", Kind: "user", Name: "Local User"}
	value, err := repository.Create(ctx, task.CreateInput{ProjectID: mustProjectIDForCurrent(t, root, database), Title: "temporary"}, actor)
	if err != nil {
		t.Fatalf("create temporary task: %v", err)
	}
	value, err = repository.Delete(ctx, value.ID, value.Revision, actor)
	if err != nil {
		t.Fatalf("delete temporary task: %v", err)
	}

	result = executeAt(t, root, database, "task", "show", "1")
	if task.ErrorCode(result.err) != task.CodeNotFound {
		t.Fatalf("show deleted task error = %v", result.err)
	}
}

func TestTaskShowRejectsZeroWithTypedError(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	executeAt(t, root, database, "init", "--json")

	result := executeAt(t, root, database, "task", "show", "0")
	if task.ErrorCode(result.err) != task.CodeInvalid {
		t.Fatalf("show task 0 error = %v, want %s", result.err, task.CodeInvalid)
	}
}

func TestTaskCLICompletionIncludesTaskSubcommands(t *testing.T) {
	options := completionSuggestionLines(t, "istok task ")

	if len(options) == 0 {
		t.Fatal("completion returned no suggestions")
	}
	want := map[string]bool{
		"list":  false,
		"show":  false,
		"claim": false,
		"done":  false,
	}
	for _, option := range options {
		if _, ok := want[option]; ok {
			want[option] = true
		}
	}
	for command, present := range want {
		if !present {
			t.Errorf("completion at \"istok task \" does not include %q: %v", command, options)
		}
	}
}

func mustProjectIDForCurrent(t *testing.T, cwd, database string) string {
	t.Helper()
	result := executeAt(t, cwd, database, "project", "show", "--json")
	if result.err != nil {
		t.Fatalf("project show for %q: %v", cwd, result.err)
	}
	return decodeJSONResult[struct {
		ID string `json:"id"`
	}](t, result.output).ID
}

func TestTaskListRendererStableBlockedBySorting(t *testing.T) {
	result := taskListBlockedBy(task.TaskListItem{
		ActiveBlockers: []task.BlockerSummary{
			{Number: 11},
			{Number: 2},
		},
	})
	result = stripANSI(result)
	if result != "#2, #11" {
		t.Fatalf("taskListBlockedBy = %q", result)
	}
}

func rowForTitle(output, title string) string {
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if strings.Contains(line, title) {
			return line
		}
	}

	return ""
}

func newTaskRepositoryForTest(t *testing.T, database string) (*taskrepo.Repository, *sql.DB) {
	t.Helper()

	ctx := context.Background()
	var repository *taskrepo.Repository
	var db *sql.DB
	app := fx.New(
		fx.NopLogger,
		bootstrap.TaskOptions(database),
		fx.Invoke(func(value *sql.DB, repo *taskrepo.Repository) {
			db = value
			repository = repo
		}),
	)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start task repository app: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Errorf("stop task repository app: %v", err)
		}
	})
	return repository, db
}

func completionSuggestionLines(t *testing.T, line string) []string {
	t.Helper()

	var command CLI
	var output bytes.Buffer
	parser, err := kong.New(
		&command,
		kong.Name("istok"),
		kong.Writers(&output, io.Discard),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		t.Fatalf("create CLI parser: %v", err)
	}

	originalLine, hasOriginalLine := os.LookupEnv("COMP_LINE")
	originalPoint, hasOriginalPoint := os.LookupEnv("COMP_POINT")
	if err := os.Setenv("COMP_LINE", line); err != nil {
		t.Fatalf("set COMP_LINE: %v", err)
	}
	if err := os.Setenv("COMP_POINT", strconv.Itoa(len(line))); err != nil {
		t.Fatalf("set COMP_POINT: %v", err)
	}

	completionHandled, err := registerCompletion(parser)
	if err != nil {
		t.Fatalf("register completion: %v", err)
	}
	if !completionHandled {
		t.Fatal("completion was not handled for completion suggestion request")
	}

	t.Cleanup(func() {
		if hasOriginalLine {
			if err := os.Setenv("COMP_LINE", originalLine); err != nil {
				t.Errorf("restore COMP_LINE: %v", err)
			}
		} else if err := os.Unsetenv("COMP_LINE"); err != nil {
			t.Errorf("unset COMP_LINE: %v", err)
		}
		if hasOriginalPoint {
			if err := os.Setenv("COMP_POINT", originalPoint); err != nil {
				t.Errorf("restore COMP_POINT: %v", err)
			}
		} else if err := os.Unsetenv("COMP_POINT"); err != nil {
			t.Errorf("unset COMP_POINT: %v", err)
		}
	})

	options := []string{}
	for _, suggestion := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if suggestion != "" {
			options = append(options, suggestion)
		}
	}

	return options
}

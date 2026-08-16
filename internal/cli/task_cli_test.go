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

	"s26.dev/istok-cli/internal/application/bootstrap"
	"s26.dev/istok-cli/internal/storage/taskrepo"
	"s26.dev/istok-cli/internal/task"
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
	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("task list output has no trailing newline:\n%s", output)
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("task list output unexpectedly short:\n%s", output)
	}
	if !strings.Contains(lines[0], "┌") || !strings.Contains(lines[len(lines)-1], "└") {
		t.Fatalf("task list output is not StyleLight table:\n%s", output)
	}
	headerLine := ""
	for _, line := range lines {
		if strings.Contains(line, "#") && strings.Contains(line, "STATE") && strings.Contains(line, "TITLE") && strings.Contains(line, "BLOCKED BY") {
			headerLine = line
			break
		}
	}
	if headerLine == "" {
		t.Fatalf("task list header line not found:\n%s", output)
	}

	for _, want := range []string{
		ready.Title,
		blocker.Title,
		blocked.Title,
		explicitBlocked.Title,
		"READY",
		"BLOCKED",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("task list output missing %q:\n%s", want, output)
		}
	}
	for _, unexpected := range []string{done.Title, deleted.Title} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("task list output unexpectedly contains %q:\n%s", unexpected, output)
		}
	}

	readyIndex := strings.Index(output, ready.Title)
	blockerIndex := strings.Index(output, blocker.Title)
	blockedIndex := strings.Index(output, blocked.Title)
	explicitIndex := strings.Index(output, explicitBlocked.Title)
	if readyIndex >= blockerIndex || blockerIndex >= blockedIndex || blockedIndex >= explicitIndex {
		t.Fatalf("task list order incorrect:\n%s", output)
	}
	readyRow := rowForTitle(output, ready.Title)
	if !strings.Contains(readyRow, "READY") {
		t.Fatalf("ready row is not READY:\n%q", readyRow)
	}
	blockedRow := rowForTitle(output, blocked.Title)
	if !strings.Contains(blockedRow, "BLOCKED") {
		t.Fatalf("blocked-by row is not BLOCKED:\n%q", blockedRow)
	}
	explicitRow := rowForTitle(output, explicitBlocked.Title)
	if !strings.Contains(explicitRow, "BLOCKED") {
		t.Fatalf("explicitly blocked row is not BLOCKED:\n%q", explicitRow)
	}
	if !strings.Contains(output, "#"+strconv.FormatInt(blocker.Number, 10)) {
		t.Fatalf("blocked task blocked-by column is missing blocker #%d: \n%s", blocker.Number, output)
	}
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("task list contains ANSI escape sequence:\n%q", output)
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
	if result.output != "No open tasks\n" {
		t.Fatalf("task list output = %q", result.output)
	}
}

func TestTaskShowCommandIsNotRegisteredYet(t *testing.T) {
	result := executeHelp(t, "task", "--help")
	if strings.Contains(result, "task show") {
		t.Fatalf("task show help unexpectedly registered: %s", result)
	}

	var output bytes.Buffer
	var errorOutput bytes.Buffer
	err := ExecuteAt(context.Background(), []string{"task", "show", "1"}, strings.NewReader(""), &output, &errorOutput, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("expected unsupported command error, got %v (%q)", err, output.String())
	}
}

func TestTaskCLICompletionIncludesListSubcommand(t *testing.T) {
	options := completionSuggestionLines(t, "istok task ")

	if len(options) == 0 {
		t.Fatal("completion returned no suggestions")
	}
	for _, option := range options {
		if option == "list" {
			return
		}
	}

	t.Fatalf("completion at \"istok task \" does not include list:\n%v", options)
}

func TestTaskListRendererStableBlockedBySorting(t *testing.T) {
	result := taskListBlockedBy(task.TaskListItem{
		ActiveBlockers: []task.BlockerSummary{
			{Number: 11},
			{Number: 2},
		},
	})
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

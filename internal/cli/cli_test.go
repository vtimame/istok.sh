package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"

	"s26.dev/istok-cli/internal/application/bootstrap"
)

func TestHelpDoesNotCreateDatabase(t *testing.T) {
	dataDir := t.TempDir()
	setXDGDataHome(t, dataDir)

	for _, args := range [][]string{
		{"--help"},
		{"project", "--help"},
		{"project", "list", "--help"},
		{"task", "--help"},
		{"task", "list", "--help"},
		{"task", "show", "--help"},
		{"completion", "zsh"},
	} {
		var output bytes.Buffer
		var errorOutput bytes.Buffer
		if err := ExecuteAt(context.Background(), args, strings.NewReader(""), &output, &errorOutput, t.TempDir()); err != nil {
			t.Fatalf("ExecuteAt(%q) error = %v", args, err)
		}
		if !strings.Contains(output.String(), "istok") {
			t.Fatalf("help output for %q = %q", args, output.String())
		}
	}

	if _, err := os.Stat(filepath.Join(dataDir, "istok", "istok.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database was unexpectedly created, stat error = %v", err)
	}
}

func TestRootHelpShowsOnlyTopLevelCommands(t *testing.T) {
	result := executeHelp(t, "--help")

	for _, command := range []string{"version", "mcp", "update", "init", "project", "task"} {
		if !strings.Contains(result, command) {
			t.Errorf("root help does not contain %q:\n%s", command, result)
		}
	}
	for _, unexpected := range []string{"project show", "project list", "project rename", "task list", "task show", "--database"} {
		if strings.Contains(result, unexpected) {
			t.Errorf("root help unexpectedly contains %q:\n%s", unexpected, result)
		}
	}
}

func TestTaskHelpShowsReadSubcommands(t *testing.T) {
	result := executeHelp(t, "task", "--help")

	if !strings.Contains(result, "task list") {
		t.Errorf("task help does not contain list command:\n%s", result)
	}
	if !strings.Contains(result, "task show") {
		t.Errorf("task help does not contain show command:\n%s", result)
	}
}

func TestTaskShowHelpContainsDatabaseAndArgumentFlags(t *testing.T) {
	result := executeHelp(t, "task", "show", "--help")
	compact := strings.Join(strings.Fields(result), " ")

	for _, want := range []string{
		"Show one task from the current project.",
		"Project-scoped task number in the current project.",
		"Path to the SQLite database",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("task show help does not contain %q:\n%s", want, result)
		}
	}
}

func TestTaskShowParseErrorDoesNotCreateDatabase(t *testing.T) {
	dataDir := t.TempDir()
	setXDGDataHome(t, dataDir)

	for _, args := range [][]string{
		{"task", "show"},
		{"task", "show", "abc"},
		{"task", "show", "-1"},
		{"task", "show", "18446744073709551616"},
	} {
		var output bytes.Buffer
		var errorOutput bytes.Buffer
		err := ExecuteAt(context.Background(), args, strings.NewReader(""), &output, &errorOutput, t.TempDir())
		if err == nil {
			t.Fatalf("expected parse error for %q, got nil", args)
		}
	}

	if _, err := os.Stat(filepath.Join(dataDir, "istok", "istok.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database was unexpectedly created, stat error = %v", err)
	}
}

func TestTaskListHelpContainsDatabaseFlag(t *testing.T) {
	result := executeHelp(t, "task", "list", "--help")
	compact := strings.Join(strings.Fields(result), " ")

	for _, want := range []string{"Path to the SQLite database", "List open tasks in the current project."} {
		if !strings.Contains(compact, want) {
			t.Errorf("task list help does not contain %q:\n%s", want, result)
		}
	}
}

func TestParseErrorDoesNotCreateDatabase(t *testing.T) {
	dataDir := t.TempDir()
	setXDGDataHome(t, dataDir)

	var output bytes.Buffer
	var errorOutput bytes.Buffer
	err := ExecuteAt(context.Background(), []string{"task", "list", "--invalid"}, strings.NewReader(""), &output, &errorOutput, t.TempDir())
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}

	if _, err := os.Stat(filepath.Join(dataDir, "istok", "istok.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database was unexpectedly created, stat error = %v", err)
	}
}

func TestProjectHelpShowsDirectSubcommandsWithDescriptions(t *testing.T) {
	result := executeHelp(t, "project", "--help")

	for _, want := range []string{
		"project show       Show one local project.",
		"project list       List local projects.",
		"project rename     Rename a local project.",
		"project rebind     Change a project's root path.",
		"project delete     Move a local project to the recoverable trash.",
		"project restore    Restore a deleted local project.",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("project help does not contain %q:\n%s", want, result)
		}
	}
}

func TestProjectSubcommandHelpDescribesArgumentsAndFlags(t *testing.T) {
	result := executeHelp(t, "project", "delete", "--help")
	compact := strings.Join(strings.Fields(result), " ")

	for _, want := range []string{
		"Project ID, unambiguous ID prefix, or name; defaults to the current project.",
		"Move to local trash without an interactive confirmation.",
		"Path to the SQLite database",
		"Write a versioned JSON response; requires --yes.",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("delete help does not contain %q:\n%s", want, result)
		}
	}

	result = executeHelp(t, "project", "restore", "--help")
	compact = strings.Join(strings.Fields(result), " ")
	for _, want := range []string{"Deleted project ID, unambiguous ID prefix, or name to restore.", "Root path to restore; omit to reuse the previous root."} {
		if !strings.Contains(compact, want) {
			t.Errorf("restore help does not contain %q:\n%s", want, result)
		}
	}
}

func TestVersionDoesNotCreateDatabase(t *testing.T) {
	dataDir := t.TempDir()
	setXDGDataHome(t, dataDir)

	var output bytes.Buffer
	var errorOutput bytes.Buffer
	if err := ExecuteAt(context.Background(), []string{"version"}, strings.NewReader(""), &output, &errorOutput, t.TempDir()); err != nil {
		t.Fatalf("ExecuteAt() error = %v", err)
	}

	if strings.TrimSpace(output.String()) == "" {
		t.Fatal("version output is empty")
	}

	if _, err := os.Stat(filepath.Join(dataDir, "istok", "istok.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database was unexpectedly created, stat error = %v", err)
	}
}

func TestProjectCLIJSONLifecycleAndNoMarkerFiles(t *testing.T) {
	root := t.TempDir()
	rebound := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")

	first := executeAt(t, root, database, "init", "--json")
	if first.err != nil {
		t.Fatalf("first init: %v", first.err)
	}
	firstInit := decodeJSONResult[struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
		Created bool `json:"created"`
	}](t, first.output)
	if !firstInit.Created || firstInit.Project.Name != filepath.Base(root) {
		t.Fatalf("first init = %+v, want created project with default basename", firstInit)
	}

	second := executeAt(t, root, database, "init", "--json")
	if second.err != nil {
		t.Fatalf("second init: %v", second.err)
	}
	if got := decodeJSONResult[struct {
		Created bool `json:"created"`
	}](t, second.output); got.Created {
		t.Fatal("second init unexpectedly created a new project")
	}

	show := executeAt(t, root, database, "project", "show", "--json")
	if show.err != nil {
		t.Fatalf("show current: %v", show.err)
	}
	shown := decodeJSONResult[struct {
		ID string `json:"id"`
	}](t, show.output)
	if shown.ID != firstInit.Project.ID {
		t.Fatalf("show ID = %q, want %q", shown.ID, firstInit.Project.ID)
	}

	listed := executeAt(t, root, database, "project", "list", "--json")
	if listed.err != nil {
		t.Fatalf("list: %v", listed.err)
	}
	if got := decodeJSONResult[[]struct {
		ID string `json:"id"`
	}](t, listed.output); len(got) != 1 || got[0].ID != firstInit.Project.ID {
		t.Fatalf("list = %+v, want one initialized project", got)
	}

	renamed := executeAt(t, root, database, "project", "rename", "renamed", "--json")
	if renamed.err != nil {
		t.Fatalf("rename: %v", renamed.err)
	}
	if got := decodeJSONResult[struct {
		Name string `json:"name"`
	}](t, renamed.output); got.Name != "renamed" {
		t.Fatalf("renamed project = %+v", got)
	}

	reboundResult := executeAt(t, rebound, database, "project", "rebind", firstInit.Project.ID, "--json")
	if reboundResult.err != nil {
		t.Fatalf("rebind: %v", reboundResult.err)
	}

	deleted := executeAt(t, rebound, database, "project", "delete", "--yes", "--json")
	if deleted.err != nil {
		t.Fatalf("delete: %v", deleted.err)
	}

	restored := executeAt(t, rebound, database, "project", "restore", firstInit.Project.ID, "--json")
	if restored.err != nil {
		t.Fatalf("restore: %v", restored.err)
	}
	if got := decodeJSONResult[struct {
		DeletedAt any `json:"deleted_at"`
	}](t, restored.output); got.DeletedAt != nil {
		t.Fatalf("restored project remains deleted: %+v", got)
	}

	for _, directory := range []string{root, rebound} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("read project root: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("project root %q has unexpected files: %+v", directory, entries)
		}
	}
}

func TestProjectCLIJSONSuccessIsPureJSON(t *testing.T) {
	root := t.TempDir()
	result := executeAt(t, root, filepath.Join(t.TempDir(), "istok.db"), "init", root, "--json")
	if result.err != nil {
		t.Fatalf("JSON init: %v", result.err)
	}
	if !json.Valid([]byte(result.output)) {
		t.Fatalf("JSON command polluted stdout with non-JSON output: %q", result.output)
	}
}

func TestProjectListHumanOutputIsTable(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	initialized := executeAt(t, root, database, "init", root, "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID

	listed := executeAt(t, root, database, "project", "list")
	if listed.err != nil {
		t.Fatalf("list: %v", listed.err)
	}

	for _, want := range []string{"ID", "NAME", "STATUS", "REVISION", "ROOT", projectID, filepath.Base(root), "active", root} {
		if !strings.Contains(listed.output, want) {
			t.Errorf("human list does not contain %q:\n%s", want, listed.output)
		}
	}
	if strings.Contains(listed.output, "\x1b[") {
		t.Fatalf("human list contains ANSI escape sequence:\n%q", listed.output)
	}
	if !strings.Contains(listed.output, "┌") || !strings.Contains(listed.output, "└") {
		t.Fatalf("human list is not a StyleLight table:\n%s", listed.output)
	}
}

func TestEmptyProjectListHumanOutputIsTable(t *testing.T) {
	listed := executeAt(t, t.TempDir(), filepath.Join(t.TempDir(), "istok.db"), "project", "list")
	if listed.err != nil {
		t.Fatalf("list: %v", listed.err)
	}

	for _, want := range []string{"ID", "NAME", "STATUS", "REVISION", "ROOT", "No projects.", "—"} {
		if !strings.Contains(listed.output, want) {
			t.Errorf("empty human list does not contain %q:\n%s", want, listed.output)
		}
	}
	if strings.Contains(listed.output, "\x1b[") {
		t.Fatalf("empty human list contains ANSI escape sequence:\n%q", listed.output)
	}
	if !strings.Contains(listed.output, "┌") || !strings.Contains(listed.output, "└") {
		t.Fatalf("empty human list is not a StyleLight table:\n%s", listed.output)
	}
}

func TestProjectCLIDeletionConfirmationAndJSONErrors(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	initialized := executeAt(t, root, database, "init", root, "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID

	jsonDelete := executeAt(t, root, database, "project", "delete", "--json")
	if jsonDelete.err == nil {
		t.Fatal("JSON delete without --yes unexpectedly succeeded")
	}
	assertVersionedBusinessError(t, jsonDelete.err, "invalid_argument")
	assertProjectExists(t, root, database, projectID)

	cancelled := executeAtWithInput(t, root, database, strings.NewReader("no\n"), "project", "delete")
	if cancelled.err != nil || !strings.Contains(cancelled.output, "cancelled") {
		t.Fatalf("cancelled delete = (%q, %v)", cancelled.output, cancelled.err)
	}
	assertProjectExists(t, root, database, projectID)

	confirmed := executeAtWithInput(t, root, database, strings.NewReader("yes\n"), "project", "delete")
	if confirmed.err != nil {
		t.Fatalf("confirmed delete: %v", confirmed.err)
	}
	deleted := executeAt(t, root, database, "project", "show", projectID, "--json")
	if deleted.err == nil {
		t.Fatal("deleted project was returned by show")
	}
	assertVersionedBusinessError(t, deleted.err, "project_not_found")
}

type executeResult struct {
	output string
	err    error
}

func executeHelp(t *testing.T, args ...string) string {
	t.Helper()
	var output, errorOutput bytes.Buffer
	if err := ExecuteAt(context.Background(), args, strings.NewReader(""), &output, &errorOutput, t.TempDir()); err != nil {
		t.Fatalf("ExecuteAt(%q): %v", args, err)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr for %q: %q", args, errorOutput.String())
	}

	return output.String()
}

func executeAt(t *testing.T, cwd, database string, args ...string) executeResult {
	t.Helper()
	return executeAtWithInput(t, cwd, database, strings.NewReader(""), args...)
}

func executeAtWithInput(t *testing.T, cwd, database string, input *strings.Reader, args ...string) executeResult {
	t.Helper()
	args = append(args, "--database", database)
	var output, errorOutput bytes.Buffer
	err := ExecuteAt(context.Background(), args, input, &output, &errorOutput, cwd)
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr for %q: %q", args, errorOutput.String())
	}
	return executeResult{output: output.String(), err: err}
}

func decodeJSONResult[T any](t *testing.T, output string) T {
	t.Helper()
	var response struct {
		SchemaVersion string `json:"schema_version"`
		Result        T      `json:"result"`
	}
	line, _, _ := strings.Cut(output, "\n")
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatalf("decode JSON output %q: %v", output, err)
	}
	if response.SchemaVersion != "1" {
		t.Fatalf("schema version = %q, want 1", response.SchemaVersion)
	}
	return response.Result
}

func assertVersionedBusinessError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(err.Error()), &payload) != nil || payload.SchemaVersion != "1" || payload.Error.Code != wantCode {
		t.Fatalf("error = %v, want versioned JSON error with code %q", err, wantCode)
	}
}

func assertProjectExists(t *testing.T, cwd, database, id string) {
	t.Helper()
	result := executeAt(t, cwd, database, "project", "show", id, "--json")
	if result.err != nil {
		t.Fatalf("project %q unexpectedly unavailable: %v", id, result.err)
	}
}

func TestMCPGraphIsValid(t *testing.T) {
	if err := ValidateMCPGraph(t.TempDir() + "/istok.db"); err != nil {
		t.Fatalf("ValidateMCPGraph() error = %v", err)
	}
}

func TestUpdateGraphIsValid(t *testing.T) {
	if err := bootstrap.ValidateUpdateGraph(); err != nil {
		t.Fatalf("ValidateUpdateGraph() error = %v", err)
	}
}

func TestRunWithShutdownTreatsCanceledRunAsNormalShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stopped := false
	err := runWithShutdown(ctx,
		func(context.Context) error { return context.Canceled },
		func(stopCtx context.Context) error {
			stopped = true
			if err := stopCtx.Err(); err != nil {
				t.Fatalf("stop context unexpectedly canceled: %v", err)
			}
			if _, ok := stopCtx.Deadline(); !ok {
				t.Fatal("stop context has no deadline")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runWithShutdown() error = %v", err)
	}
	if !stopped {
		t.Fatal("stop was not called")
	}
}

func TestRunWithShutdownJoinsRunAndStopErrors(t *testing.T) {
	runErr := errors.New("run failure")
	stopErr := errors.New("stop failure")
	err := runWithShutdown(context.Background(),
		func(context.Context) error { return runErr },
		func(context.Context) error { return stopErr },
	)
	if !errors.Is(err, runErr) || !errors.Is(err, stopErr) {
		t.Fatalf("runWithShutdown() error = %v, want joined run and stop errors", err)
	}
}

func setXDGDataHome(t *testing.T, directory string) {
	t.Helper()
	previous, wasSet := os.LookupEnv("XDG_DATA_HOME")
	if err := os.Setenv("XDG_DATA_HOME", directory); err != nil {
		t.Fatalf("set XDG_DATA_HOME: %v", err)
	}
	xdg.Reload()

	t.Cleanup(func() {
		if wasSet {
			if err := os.Setenv("XDG_DATA_HOME", previous); err != nil {
				t.Errorf("restore XDG_DATA_HOME: %v", err)
			}
		} else if err := os.Unsetenv("XDG_DATA_HOME"); err != nil {
			t.Errorf("unset XDG_DATA_HOME: %v", err)
		}

		xdg.Reload()
	})
}

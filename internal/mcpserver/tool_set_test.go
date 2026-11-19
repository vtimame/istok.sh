package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vtimame/istok.sh/internal/task"
)

func TestCoreToolSetListsTheShortWorkflowAndRejectsHiddenTools(t *testing.T) {
	for _, profile := range []Profile{Worker, Supervisor, Admin} {
		t.Run(string(profile), func(t *testing.T) {
			database := filepath.Join(t.TempDir(), "core.db")
			_, session := newSessionWithTools(t, profile, ToolsCore, t.TempDir(), database, "core-agent", nil)

			listed, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}

			var got []string
			for _, value := range listed.Tools {
				got = append(got, value.Name)
			}
			var want []string
			for name := range coreTools {
				want = append(want, name)
			}
			want = append(want, profileTools[profile]...)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("core tools = %v, want %v", got, want)
			}

			hidden := callTool(t, session, "task_claim", map[string]any{"task_id": "x"})
			if !hidden.IsError || len(hidden.Content) != 1 {
				t.Fatalf("hidden tool result = %+v", hidden)
			}
			var failure ErrorResult
			if err := json.Unmarshal([]byte(hidden.Content[0].(*mcp.TextContent).Text), &failure); err != nil {
				t.Fatal(err)
			}
			if failure.Error.Code != codeToolUnavailable || !strings.Contains(failure.Error.Message, "--tools full") {
				t.Fatalf("hidden tool error = %+v", failure.Error)
			}

			initProject(t, session, "core")
		})
	}
}

func TestInstructionsNameOnlyCoreTools(t *testing.T) {
	_, session := newSessionWithTools(t, Worker, ToolsCore, t.TempDir(), filepath.Join(t.TempDir(), "core.db"), "core-agent", nil)

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	available := map[string]bool{}
	for _, value := range listed.Tools {
		available[value.Name] = true
	}

	for _, name := range instructionToolNames() {
		if !available[name] {
			t.Errorf("instructions name %q, which the core tool set does not list", name)
		}
	}
}

func TestTaskBlockWithoutRevision(t *testing.T) {
	_, session := newSession(t, Worker, t.TempDir())
	initProject(t, session, "block")

	started := startTask(t, session, map[string]any{"title": "needs a decision"})
	blocked := callTaskResult(t, session, "task_block", map[string]any{"task_id": started.Task.ID, "reason": "waiting for the schema decision"})
	if blocked.Task.Status != task.StatusBlocked {
		t.Fatalf("task_block without revision = %+v", blocked.Task)
	}
}

func TestTaskCommitLinksACommitOnce(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "api")
	commitIn(t, nested, "feat: api change")

	_, session := newSession(t, Worker, root)
	initProject(t, session, "commits")
	started := startTask(t, session, map[string]any{"title": "ship the api"})

	first := callCommit(t, session, map[string]any{"task_id": started.Task.ID, "repository": "api"})
	if !first.Linked || first.Commit == nil || first.Commit.Subject != "feat: api change" || first.Commit.Repository != "api" || len(first.Commit.SHA) != 40 {
		t.Fatalf("first task_commit = %+v", first)
	}

	again := callCommit(t, session, map[string]any{"task_id": started.Task.ID, "repository": "api", "commit": first.Commit.SHA})
	if again.Linked {
		t.Fatalf("second task_commit linked again: %+v", again)
	}

	shown := callTool(t, session, "task_show", map[string]any{"task_id": started.Task.ID})
	var show TaskShowResult
	decodeStructured(t, shown, &show)
	var commits []task.Commit
	for _, event := range show.Show.Events {
		if commit, ok := task.CommitOf(event); ok {
			commits = append(commits, commit)
		}
	}
	if len(commits) != 1 || commits[0].SHA != first.Commit.SHA {
		t.Fatalf("commit events = %+v", commits)
	}

	outside := callTool(t, session, "task_commit", map[string]any{"task_id": started.Task.ID, "repository": "../elsewhere"})
	if !outside.IsError {
		t.Fatalf("task_commit outside the root = %+v", outside)
	}
}

func callCommit(t *testing.T, session *mcp.ClientSession, arguments map[string]any) TaskCommitResult {
	t.Helper()

	result := callTool(t, session, "task_commit", arguments)
	if result.IsError {
		t.Fatalf("task_commit error = %+v", result.Content)
	}

	var value TaskCommitResult
	decodeStructured(t, result, &value)

	return value
}

// commitIn creates a git repository in directory with one commit.
func commitIn(t *testing.T, directory, subject string) {
	t.Helper()

	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) {
		command := exec.Command("git", append([]string{"-C", directory}, args...)...)
		command.Env = append(command.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	run("init", "-q")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "--allow-empty", "-m", subject)
}

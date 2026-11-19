package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vtimame/istok.sh/internal/task"
)

func TestHookPrintsInterruptedWorkAndAsksOnceForAHandoff(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	t.Setenv("ISTOK_AGENT_ID", "")

	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID

	tasks, _ := newTaskRepositoryForTest(t, database)
	actor := task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}
	created, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "hooked work"}, actor)
	if err != nil {
		t.Fatal(err)
	}

	elsewhere := t.TempDir()
	input := func(fields string) *strings.Reader {
		encoded, _ := json.Marshal(root)
		return strings.NewReader(`{"cwd":` + string(encoded) + fields + `}`)
	}

	quiet := executeAtWithInput(t, elsewhere, database, input(""), "hook", "claude", "session-start")
	if quiet.err != nil || quiet.output != "" {
		t.Fatalf("session-start without active runs = %q, %v", quiet.output, quiet.err)
	}

	if claimed := executeAt(t, root, database, "task", "claim", "1", "--json"); claimed.err != nil {
		t.Fatalf("task claim: %v", claimed.err)
	}

	started := executeAtWithInput(t, elsewhere, database, input(`,"source":"compact"`), "hook", "claude", "session-start")
	if started.err != nil || !strings.Contains(started.output, "Task #1 hooked work") || !strings.Contains(started.output, created.ID) {
		t.Fatalf("session-start = %q, %v", started.output, started.err)
	}

	compacting := executeAtWithInput(t, elsewhere, database, input(`,"trigger":"auto"`), "hook", "claude", "pre-compact")
	if compacting.err != nil || !strings.HasPrefix(compacting.output, "Istok tracks the work below") || !strings.Contains(compacting.output, created.ID) {
		t.Fatalf("pre-compact = %q, %v", compacting.output, compacting.err)
	}

	stopped := executeAtWithInput(t, elsewhere, database, input(""), "hook", "codex", "stop")
	var decision struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(stopped.output), &decision); err != nil || decision.Decision != "block" || !strings.Contains(decision.Reason, "task_handoff") {
		t.Fatalf("stop = %q, %v", stopped.output, stopped.err)
	}

	again := executeAtWithInput(t, elsewhere, database, input(`,"stop_hook_active":true`), "hook", "claude", "stop")
	if again.err != nil || strings.TrimSpace(again.output) != "{}" {
		t.Fatalf("stop while continuing = %q, %v", again.output, again.err)
	}

	current, err := tasks.Resolve(context.Background(), task.Selector{ProjectID: projectID, ID: created.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Handoff(context.Background(), created.ID, current.Revision, "done: hooks\nnext: docs", actor); err != nil {
		t.Fatal(err)
	}

	handedOff := executeAtWithInput(t, elsewhere, database, input(""), "hook", "claude", "stop")
	if handedOff.err != nil || strings.TrimSpace(handedOff.output) != "{}" {
		t.Fatalf("stop after a handoff = %q, %v", handedOff.output, handedOff.err)
	}
}

func TestHookOutsideAProjectIsSilent(t *testing.T) {
	database := filepath.Join(t.TempDir(), "istok.db")

	started := executeAtWithInput(t, t.TempDir(), database, strings.NewReader(""), "hook", "claude", "session-start")
	if started.err != nil || started.output != "" {
		t.Fatalf("session-start outside a project = %q, %v", started.output, started.err)
	}

	stopped := executeAtWithInput(t, t.TempDir(), database, strings.NewReader(""), "hook", "codex", "stop")
	if stopped.err != nil || strings.TrimSpace(stopped.output) != "{}" {
		t.Fatalf("stop outside a project = %q, %v", stopped.output, stopped.err)
	}
}

func TestHooksInstallWritesTheAgentSettings(t *testing.T) {
	previous := hookExecutable
	hookExecutable = func() (string, error) { return "/usr/local/bin/istok", nil }
	t.Cleanup(func() { hookExecutable = previous })

	claudeHome := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("CODEX_HOME", codexHome)
	database := filepath.Join(t.TempDir(), "istok.db")

	installed := executeAt(t, t.TempDir(), database, "hooks", "install", "claude", "--stop", "--json")
	if installed.err != nil {
		t.Fatalf("hooks install: %v", installed.err)
	}
	change := decodeJSONResult[struct {
		Path    string   `json:"path"`
		Changed bool     `json:"changed"`
		Events  []string `json:"events"`
	}](t, installed.output)
	if change.Path != filepath.Join(claudeHome, "settings.json") || !change.Changed || strings.Join(change.Events, ",") != "session-start,pre-compact,post-tool-use,stop" {
		t.Fatalf("hooks install = %+v", change)
	}

	if codex := executeAt(t, t.TempDir(), database, "hooks", "install", "codex"); codex.err != nil || !strings.Contains(codex.output, "/hooks") {
		t.Fatalf("codex install = %q, %v", codex.output, codex.err)
	}

	status := executeAt(t, t.TempDir(), database, "hooks", "status")
	if status.err != nil || !strings.Contains(status.output, "session-start, pre-compact, post-tool-use, stop") || !strings.Contains(status.output, filepath.Join(codexHome, "hooks.json")+": session-start") {
		t.Fatalf("hooks status = %q, %v", status.output, status.err)
	}

	if removed := executeAt(t, t.TempDir(), database, "hooks", "uninstall", "claude"); removed.err != nil || !strings.Contains(removed.output, "Istok hooks: none") {
		t.Fatalf("hooks uninstall = %q, %v", removed.output, removed.err)
	}

	if project := executeAt(t, t.TempDir(), database, "hooks", "install", "claude", "--scope", "project"); project.err == nil {
		t.Fatalf("project scope outside a project succeeded: %q", project.output)
	}
}

func TestCommitHookLinksTheCommitToTheOnlyActiveRun(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	t.Setenv("ISTOK_AGENT_ID", "")

	initialized := executeAt(t, root, database, "init", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized.output).Project.ID

	tasks, _ := newTaskRepositoryForTest(t, database)
	actor := task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}
	created, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "committed work"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if claimed := executeAt(t, root, database, "task", "claim", "1", "--json"); claimed.err != nil {
		t.Fatalf("task claim: %v", claimed.err)
	}

	web := filepath.Join(root, "web")
	gitCommitIn(t, web, "chore: start over")
	gitIn(t, web, "commit", "-q", "--allow-empty", "-m", "feat: web change")

	input := func(command string) *strings.Reader {
		encoded, _ := json.Marshal(map[string]any{"cwd": root, "tool_name": "Bash", "tool_input": map[string]string{"command": command}})
		return strings.NewReader(string(encoded))
	}

	other := executeAtWithInput(t, root, database, input("go test ./..."), "hook", "claude", "post-tool-use")
	if other.err != nil || other.output != "" {
		t.Fatalf("hook after another command = %q, %v", other.output, other.err)
	}

	linked := executeAtWithInput(t, root, database, input("git -C web commit -m 'feat: web change'"), "hook", "claude", "post-tool-use")
	var output struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(linked.output), &output); err != nil || output.HookSpecificOutput.HookEventName != "PostToolUse" || !strings.Contains(output.HookSpecificOutput.AdditionalContext, "task #1") {
		t.Fatalf("commit hook = %q, %v", linked.output, linked.err)
	}

	again := executeAtWithInput(t, root, database, input("git commit --amend --no-edit"), "hook", "claude", "post-tool-use")
	if again.err != nil || again.output != "" {
		t.Fatalf("hook for a linked commit = %q, %v", again.output, again.err)
	}

	shown, err := tasks.Show(context.Background(), task.Selector{ProjectID: projectID, ID: created.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	var commits []task.Commit
	for _, event := range shown.Events {
		if commit, ok := task.CommitOf(event); ok {
			commits = append(commits, commit)
		}
	}
	if len(commits) != 2 || commits[0].Subject != "chore: start over" || commits[1].Subject != "feat: web change" || commits[1].Repository != "web" {
		t.Fatalf("linked commits = %+v", commits)
	}

	gitIn(t, web, "commit", "-q", "--amend", "--allow-empty", "-m", "feat: web change, reworded")
	gitIn(t, web, "reflog", "expire", "--expire=now", "--all")

	shownText := executeAt(t, root, database, "task", "show", "1")
	if shownText.err != nil || !strings.Contains(shownText.output, commits[0].SHA[:12]) || strings.Count(shownText.output, "Not on any branch any more") != 1 {
		t.Fatalf("task show after a rewrite = %q, %v", shownText.output, shownText.err)
	}
}

func TestGitCommitCommandPattern(t *testing.T) {
	cases := map[string]bool{
		"git commit -m x":                true,
		"cd api && git commit -am 'x'":   true,
		"git -C web commit -m x":         true,
		"git -c user.name=x commit -m y": true,
		"git status && git log":          false,
		"echo git committee":             false,
		"git show HEAD --format=commit":  false,
	}
	for command, want := range cases {
		if got := gitCommitCommand.MatchString(command); got != want {
			t.Errorf("gitCommitCommand(%q) = %v, want %v", command, got, want)
		}
	}
}

// gitCommitIn creates a git repository in directory with one commit.
func gitCommitIn(t *testing.T, directory, subject string) {
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

// gitIn runs git in directory as a test user.
func gitIn(t *testing.T, directory string, args ...string) {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", directory, "-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)...)
	command.Env = append(command.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

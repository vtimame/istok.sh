package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	handoffapp "github.com/vtimame/istok.sh/internal/application/handoff"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/gitcommit"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/task"
)

// gitCommitCommand matches git commands that make or rewrite commits, also
// with global options such as `git -C repo commit`: commit, commit-tree with
// reset, rebase, cherry-pick, merge, am, pull and revert.
var gitCommitCommand = regexp.MustCompile(`\bgit\s+(?:-\S+\s+(?:[^-\s]\S*\s+)?)*(?:commit|commit-tree|reset|rebase|cherry-pick|merge|am|pull|revert)(?:$|[\s;&|)])`)

// commitWindow is how recent a HEAD commit must be to count as the one the
// agent just made.
const commitWindow = 10 * time.Minute

// hookActor writes the commits the hook links.
var hookActor = task.ActorSnapshot{ID: "istok-hook", Kind: "hook", Name: "Istok hook"}

// linkCommitHook links the commits an agent's `git commit` just made to the
// task of the project's only live run. With several live runs it cannot tell
// the task, so it asks the agent to call task_commit.
func linkCommitHook(ctx context.Context, database, cwd string, input hookInput) (string, error) {
	if input.ToolName != "Bash" || !gitCommitCommand.MatchString(input.ToolInput.Command) {
		return "", nil
	}

	var message string
	err := withTaskServices(ctx, database, func(projects *project.Service, tasks *taskapp.Service, runs *runapp.Service) error {
		current, err := projects.Current(ctx, cwd)
		if err != nil {
			if project.ErrorCode(err) == project.CodeNotFound {
				return nil
			}

			return err
		}
		if current.Root == nil {
			return nil
		}

		commits, err := gitcommit.Recent(ctx, current.Root.CanonicalPath, time.Now().Add(-commitWindow))
		if err != nil || len(commits) == 0 {
			return err
		}

		briefs, err := handoffapp.NewService(tasks, runs).Resume(ctx, handoffapp.ResumeInput{ProjectID: current.ID, ActorID: os.Getenv("ISTOK_AGENT_ID")})
		if err != nil {
			return err
		}

		var live []handoffapp.Brief
		for _, brief := range briefs {
			if brief.ActiveRun != nil && !brief.ActiveRun.LeaseExpired {
				live = append(live, brief)
			}
		}
		if len(live) > 1 {
			message = "Istok: several tasks have active runs, so the commit was not linked. Call task_commit with the task_id of the task it belongs to."
			return nil
		}
		if len(live) == 0 {
			return nil
		}

		owner := live[0].Task
		var linked []string
		for _, commit := range commits {
			_, added, err := tasks.LinkCommit(ctx, task.Selector{ProjectID: current.ID, ID: owner.ID}, commit, hookActor)
			if err != nil {
				return err
			}
			if added {
				linked = append(linked, fmt.Sprintf("%s %q (%s)", commit.SHA[:12], commit.Subject, commit.Repository))
			}
		}
		if len(linked) > 0 {
			message = fmt.Sprintf("Istok linked %s to task #%d %q; task_commit is not needed for it.", strings.Join(linked, ", "), owner.Number, owner.Title)
		}

		return nil
	})
	if err != nil || message == "" {
		return "", err
	}

	return postToolUseContext(message)
}

// postToolUseContext is the hook output that adds message to the agent's
// context after the tool result.
func postToolUseContext(message string) (string, error) {
	encoded, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]string{
			"hookEventName":     "PostToolUse",
			"additionalContext": message,
		},
	})
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}

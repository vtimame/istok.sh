package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/gitcommit"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/task"
)

type taskCommitInput struct {
	TaskID     string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	Repository string `json:"repository,omitempty" jsonschema:"Repository folder relative to the project root, for projects with several repositories. Defaults to the root."`
	Commit     string `json:"commit,omitempty" jsonschema:"Commit sha or revision. Defaults to HEAD."`
}

type TaskCommitResult struct {
	SchemaVersion string       `json:"schema_version"`
	Task          *task.Task   `json:"task,omitempty"`
	Commit        *task.Commit `json:"commit,omitempty"`

	// Linked is false when the task already had this commit.
	Linked bool       `json:"linked"`
	Error  *ToolError `json:"error,omitempty"`
}

func addCommitTools(server *mcp.Server, projects *project.Service, tasks *taskapp.Service, runs *runapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("task_commit", "Link a git commit to the task: sha, subject and time are read from the repository. Use repository for a nested repository of the project. Linking the same commit twice changes nothing. Keeps your run lease alive.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskCommitInput) (*mcp.CallToolResult, TaskCommitResult, error) {
		fail := func(err error) (*mcp.CallToolResult, TaskCommitResult, error) {
			return errorTool(err), TaskCommitResult{SchemaVersion: "1", Error: toolError(err)}, nil
		}

		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return fail(err)
		}
		selector, err := taskSelector(current.ID, in.TaskID)
		if err != nil {
			return fail(err)
		}

		commit, err := gitcommit.Inspect(ctx, root, in.Repository, in.Commit)
		if err != nil {
			return fail(err)
		}

		value, linked, err := tasks.LinkCommit(ctx, selector, commit, actor.task())
		if err != nil {
			return fail(err)
		}
		runs.TouchTask(ctx, value.ID, actor.run())

		return nil, TaskCommitResult{SchemaVersion: "1", Task: &value, Commit: &commit, Linked: linked}, nil
	})
}

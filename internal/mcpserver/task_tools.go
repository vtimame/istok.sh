package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/task"
)

func addTaskTools(server *mcp.Server, projects *project.Service, tasks *taskapp.Service, runs *runapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("task_create", "Create a task in the current project.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskCreateInput) (*mcp.CallToolResult, TaskResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		taskID := in.TaskID
		if taskID == "" {
			generated, err := task.NewID()
			if err != nil {
				return errorTool(err), taskErrorResult(err), nil
			}
			taskID = generated
		}
		if !task.IsUUIDv7(taskID) {
			err := task.NewError(task.CodeInvalid, "task_id must be a canonical UUIDv7")
			return errorTool(err), taskErrorResult(err), nil
		}
		input := task.CreateInput{
			ID:                 taskID,
			ProjectID:          current.ID,
			Title:              in.Title,
			Description:        in.Description,
			AcceptanceCriteria: in.AcceptanceCriteria,
			Notes:              in.Notes,
		}

		value, err := tasks.Create(ctx, input, actor.task())
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		return nil, taskResult(value), nil
	})
	mcp.AddTool(server, tool("task_update", "Change the title, description, acceptance criteria or notes of a task in the current project. Pass the revision from task_show as expected_revision; fields you omit stay as they are. Status changes go through task_block, task_done and the other task tools.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskUpdateInput) (*mcp.CallToolResult, TaskResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		selector, err := taskSelector(current.ID, in.TaskID)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		patch := task.Patch{
			Title:              in.Title,
			Description:        in.Description,
			AcceptanceCriteria: in.AcceptanceCriteria,
			Notes:              in.Notes,
		}

		value, err := tasks.Update(ctx, selector, in.ExpectedRevision, patch, actor.task())
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		return nil, taskResult(value), nil
	})
	mcp.AddTool(server, tool("task_list", "List tasks in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskListInput) (*mcp.CallToolResult, TaskListResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return errorTool(err), taskListErrorResult(err), nil
		}
		values, err := tasks.List(ctx, current.ID, task.ListOptions{Statuses: in.Statuses, IncludeDeleted: in.IncludeDeleted})
		if err != nil {
			return errorTool(err), taskListErrorResult(err), nil
		}
		return nil, taskListResult(values, nil), nil
	})
	mcp.AddTool(server, tool("task_show", "Read one task of the current project with its full history: the task fields and revision, every event (progress, comments, runs, checks, commits), its dependencies, blockers and dependents, and whether it has an active or expired run. Use it to review a task, or to read its current revision for compare-and-swap, such as task_block's optional expected_revision or task_update, which is listed when Istok runs with --tools full; to start working on the task, call task_start instead, which also returns the context and handoff you need.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskShowInput) (*mcp.CallToolResult, TaskShowResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return errorTool(err), taskShowErrorResult(err), nil
		}
		selector, err := taskSelector(current.ID, in.TaskID)
		if err != nil {
			return errorTool(err), taskShowErrorResult(err), nil
		}
		value, err := tasks.Show(ctx, selector, in.IncludeDeleted)
		if err != nil {
			return errorTool(err), taskShowErrorResult(err), nil
		}
		return nil, TaskShowResult{SchemaVersion: taskSchemaVersion, Show: &value}, nil
	})
	mcp.AddTool(server, tool("task_ready", "List ready tasks in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, TaskListResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return errorTool(err), taskListErrorResult(err), nil
		}
		values, err := tasks.Ready(ctx, current.ID)
		if err != nil {
			return errorTool(err), taskListErrorResult(err), nil
		}
		return nil, taskListResult(values, nil), nil
	})
	addTaskMutationTools(server, projects, tasks, runs, root, actor)
}

func addTaskMutationTools(server *mcp.Server, projects *project.Service, tasks *taskapp.Service, runs *runapp.Service, root string, actor actorIdentity) {
	// keepRunAlive extends the actor's lease after a note, so writing
	// progress is enough to keep a claimed task.
	keepRunAlive := func(ctx context.Context) func(task.Task, error) (task.Task, error) {
		return func(value task.Task, err error) (task.Task, error) {
			if err == nil {
				runs.TouchTask(ctx, value.ID, actor.run())
			}

			return value, err
		}
	}

	addTaskTextTool(server, "task_comment", "Add a task comment. Omit expected_revision to append without compare-and-swap.", projects, tasks, root, func(ctx context.Context, selector task.Selector, expected int64, body string) (task.Task, error) {
		return keepRunAlive(ctx)(tasks.Comment(ctx, selector, expected, body, actor.task()))
	})
	addTaskTextTool(server, "task_progress", "Add a task progress update. Omit expected_revision to append without compare-and-swap. Keeps your run lease alive.", projects, tasks, root, func(ctx context.Context, selector task.Selector, expected int64, body string) (task.Task, error) {
		return keepRunAlive(ctx)(tasks.Progress(ctx, selector, expected, body, actor.task()))
	})
	mcp.AddTool(server, tool("task_handoff", "Record where the work stands for the next session: done, where it stopped, decisions, next step. The latest handoff is current; task_start returns it. Keeps your run lease alive; with release, ends your run instead and leaves the task open for another agent.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in taskHandoffInput) (*mcp.CallToolResult, TaskHandoffResult, error) {
		failed := func(err error) (*mcp.CallToolResult, TaskHandoffResult, error) {
			return errorTool(err), TaskHandoffResult{SchemaVersion: taskSchemaVersion, Error: toolError(err)}, nil
		}

		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return failed(err)
		}
		selector, err := taskSelector(current.ID, in.TaskID)
		if err != nil {
			return failed(err)
		}

		value, err := tasks.Handoff(ctx, selector, in.Note, actor.task())
		if err != nil {
			return failed(err)
		}
		if !in.Release {
			runs.TouchTask(ctx, value.ID, actor.run())
			return nil, TaskHandoffResult{SchemaVersion: taskSchemaVersion, Task: &value}, nil
		}

		released, err := runs.ReleaseTask(ctx, selector, "handed over: see the latest handoff", actor.run())
		if err != nil {
			return failed(err)
		}

		// Ending the run changed the task's revision.
		if released != nil {
			if refreshed, err := tasks.Show(ctx, selector, false); err == nil {
				value = refreshed.Task
			}
		}

		return nil, TaskHandoffResult{SchemaVersion: taskSchemaVersion, Task: &value, Released: released}, nil
	})
	mcp.AddTool(server, tool("task_block", "Block a task with the reason. Omit expected_revision to block the current revision. Keeps your run lease alive.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskBlockInput) (*mcp.CallToolResult, TaskResult, error) {
		return mutateTask(ctx, projects, root, in.TaskID, func(selector task.Selector) (task.Task, error) {
			return keepRunAlive(ctx)(tasks.Block(ctx, selector, in.ExpectedRevision, in.Reason, actor.task()))
		})
	})
	mcp.AddTool(server, tool("task_unblock", "Unblock a task.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskUnblockInput) (*mcp.CallToolResult, TaskResult, error) {
		return mutateTask(ctx, projects, root, in.TaskID, func(selector task.Selector) (task.Task, error) {
			return tasks.Unblock(ctx, selector, in.ExpectedRevision, in.Note, actor.task())
		})
	})
	addDependencyTool(server, "task_dependency_add", "Add a task dependency.", projects, tasks, root, func(ctx context.Context, blocker, blocked task.Selector, expected int64) (task.Task, error) {
		return tasks.AddDependency(ctx, blocker, blocked, expected, actor.task())
	})
	addDependencyTool(server, "task_dependency_remove", "Remove a task dependency.", projects, tasks, root, func(ctx context.Context, blocker, blocked task.Selector, expected int64) (task.Task, error) {
		return tasks.RemoveDependency(ctx, blocker, blocked, expected, actor.task())
	})
}

func addTaskTextTool(server *mcp.Server, name, description string, projects *project.Service, tasks *taskapp.Service, root string, action func(context.Context, task.Selector, int64, string) (task.Task, error)) {
	mcp.AddTool(server, tool(name, description, false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskTextInput) (*mcp.CallToolResult, TaskResult, error) {
		return mutateTask(ctx, projects, root, in.TaskID, func(selector task.Selector) (task.Task, error) {
			return action(ctx, selector, in.ExpectedRevision, in.Body)
		})
	})
}

func addDependencyTool(server *mcp.Server, name, description string, projects *project.Service, tasks *taskapp.Service, root string, action func(context.Context, task.Selector, task.Selector, int64) (task.Task, error)) {
	destructive := name == "task_dependency_remove"
	mcp.AddTool(server, tool(name, description, false, destructive, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskDependencyInput) (*mcp.CallToolResult, TaskResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		blocker, err := taskSelector(current.ID, in.BlockerTaskID)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		blocked, err := taskSelector(current.ID, in.BlockedTaskID)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		value, err := action(ctx, blocker, blocked, in.ExpectedRevision)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		return nil, taskResult(value), nil
	})
}

func mutateTask(ctx context.Context, projects *project.Service, root, taskID string, action func(task.Selector) (task.Task, error)) (*mcp.CallToolResult, TaskResult, error) {
	current, err := currentTaskProject(ctx, projects, root)
	if err != nil {
		return errorTool(err), taskErrorResult(err), nil
	}
	selector, err := taskSelector(current.ID, taskID)
	if err != nil {
		return errorTool(err), taskErrorResult(err), nil
	}
	value, err := action(selector)
	if err != nil {
		return errorTool(err), taskErrorResult(err), nil
	}
	return nil, taskResult(value), nil
}

func addAdminTaskTools(server *mcp.Server, projects *project.Service, tasks *taskapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("task_delete", "Move a task to local trash.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskRevisionInput) (*mcp.CallToolResult, TaskResult, error) {
		return mutateTask(ctx, projects, root, in.TaskID, func(selector task.Selector) (task.Task, error) {
			return tasks.Delete(ctx, selector, in.ExpectedRevision, actor.task())
		})
	})
	mcp.AddTool(server, tool("task_restore", "Restore a task from local trash.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskRevisionInput) (*mcp.CallToolResult, TaskResult, error) {
		return mutateTask(ctx, projects, root, in.TaskID, func(selector task.Selector) (task.Task, error) {
			return tasks.Restore(ctx, selector, in.ExpectedRevision, actor.task())
		})
	})
}

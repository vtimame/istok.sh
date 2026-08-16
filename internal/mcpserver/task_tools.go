package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	taskapp "s26.dev/istok-cli/internal/application/task"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/task"
)

func addTaskTools(server *mcp.Server, projects *project.Service, tasks *taskapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("task_create", "Create a task in the current project.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskCreateInput) (*mcp.CallToolResult, TaskResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return errorTool(err), taskErrorResult(err), nil
		}
		if !task.IsUUIDv7(in.TaskID) {
			err := task.NewError(task.CodeInvalid, "task_id must be a canonical UUIDv7")
			return errorTool(err), taskErrorResult(err), nil
		}
		input := task.CreateInput{
			ID:                 in.TaskID,
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
	mcp.AddTool(server, tool("task_update", "Update a task in the current project.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskUpdateInput) (*mcp.CallToolResult, TaskResult, error) {
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
	mcp.AddTool(server, tool("task_show", "Show a task in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskShowInput) (*mcp.CallToolResult, TaskShowResult, error) {
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
	addTaskMutationTools(server, projects, tasks, root, actor)
}

func addTaskMutationTools(server *mcp.Server, projects *project.Service, tasks *taskapp.Service, root string, actor actorIdentity) {
	addTaskTextTool(server, "task_comment", "Add a task comment.", projects, tasks, root, func(ctx context.Context, selector task.Selector, expected int64, body string) (task.Task, error) {
		return tasks.Comment(ctx, selector, expected, body, actor.task())
	})
	addTaskTextTool(server, "task_progress", "Add a task progress update.", projects, tasks, root, func(ctx context.Context, selector task.Selector, expected int64, body string) (task.Task, error) {
		return tasks.Progress(ctx, selector, expected, body, actor.task())
	})
	mcp.AddTool(server, tool("task_block", "Block a task.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in taskBlockInput) (*mcp.CallToolResult, TaskResult, error) {
		return mutateTask(ctx, projects, root, in.TaskID, func(selector task.Selector) (task.Task, error) {
			return tasks.Block(ctx, selector, in.ExpectedRevision, in.Reason, actor.task())
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

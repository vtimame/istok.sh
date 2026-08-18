package mcpserver

import (
	"context"

	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/task"
)

func currentTaskProject(ctx context.Context, projects *project.Service, root string) (project.Project, error) {
	return projects.Current(ctx, root)
}

func taskSelector(projectID, taskID string) (task.Selector, error) {
	if !task.IsUUIDv7(taskID) {
		return task.Selector{}, task.NewError(task.CodeInvalid, "task_id must be a canonical UUIDv7")
	}

	return task.Selector{ProjectID: projectID, ID: taskID}, nil
}

func taskResult(value task.Task) TaskResult {
	return TaskResult{SchemaVersion: taskSchemaVersion, Task: &value}
}

func taskErrorResult(err error) TaskResult {
	return TaskResult{SchemaVersion: taskSchemaVersion, Error: toolError(err)}
}

func taskListErrorResult(err error) TaskListResult {
	return taskListResult(nil, toolError(err))
}

func taskListResult(values []task.TaskListItem, toolErr *ToolError) TaskListResult {
	if values == nil {
		values = []task.TaskListItem{}
	}

	return TaskListResult{SchemaVersion: taskSchemaVersion, Tasks: values, Error: toolErr}
}

func taskShowErrorResult(err error) TaskShowResult {
	return TaskShowResult{SchemaVersion: taskSchemaVersion, Error: toolError(err)}
}

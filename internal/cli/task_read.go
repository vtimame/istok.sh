package cli

import (
	"context"

	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/task"
)

type taskProjectResolver interface {
	Current(context.Context, string) (project.Project, error)
}

type taskReadService interface {
	List(context.Context, string, task.ListOptions) ([]task.TaskListItem, error)
	Show(context.Context, task.Selector, bool) (task.Show, error)
}

type taskReadResolver struct {
	projects taskProjectResolver
	tasks    taskReadService
}

type taskReadAPI interface {
	List(ctx context.Context, cwd string) ([]task.TaskListItem, error)
	Show(ctx context.Context, cwd string, command TaskShowCommand) (task.Show, error)
}

func newTaskReadResolver(projects taskProjectResolver, tasks taskReadService) taskReadAPI {
	return taskReadResolver{projects: projects, tasks: tasks}
}

func (r taskReadResolver) List(ctx context.Context, cwd string) ([]task.TaskListItem, error) {
	current, err := r.projects.Current(ctx, cwd)
	if err != nil {
		return nil, err
	}

	return r.tasks.List(ctx, current.ID, task.ListOptions{
		Statuses: []task.Status{task.StatusOpen, task.StatusBlocked},
	})
}

func (r taskReadResolver) Show(ctx context.Context, cwd string, command TaskShowCommand) (task.Show, error) {
	current, err := r.projects.Current(ctx, cwd)
	if err != nil {
		return task.Show{}, err
	}

	return r.tasks.Show(ctx, task.Selector{
		ProjectID: current.ID,
		Number:    command.ID,
	}, false)
}

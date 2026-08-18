package cli

import (
	"context"

	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/task"
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

type taskListView struct {
	Project project.Project
	Tasks   []task.TaskListItem
}

type taskShowView struct {
	Project project.Project
	Task    task.Show
}

type taskReadAPI interface {
	List(ctx context.Context, cwd string) (taskListView, error)
	Show(ctx context.Context, cwd string, command TaskShowCommand) (taskShowView, error)
}

func newTaskReadResolver(projects taskProjectResolver, tasks taskReadService) taskReadAPI {
	return taskReadResolver{projects: projects, tasks: tasks}
}

func (r taskReadResolver) List(ctx context.Context, cwd string) (taskListView, error) {
	current, err := r.projects.Current(ctx, cwd)
	if err != nil {
		return taskListView{}, err
	}

	values, err := r.tasks.List(ctx, current.ID, task.ListOptions{
		Statuses: []task.Status{task.StatusOpen, task.StatusBlocked},
	})
	if err != nil {
		return taskListView{}, err
	}

	return taskListView{Project: current, Tasks: values}, nil
}

func (r taskReadResolver) Show(ctx context.Context, cwd string, command TaskShowCommand) (taskShowView, error) {
	current, err := r.projects.Current(ctx, cwd)
	if err != nil {
		return taskShowView{}, err
	}

	value, err := r.tasks.Show(ctx, task.Selector{
		ProjectID: current.ID,
		Number:    command.ID,
	}, false)
	if err != nil {
		return taskShowView{}, err
	}

	return taskShowView{Project: current, Task: value}, nil
}

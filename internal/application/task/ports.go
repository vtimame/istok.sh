package taskapp

import (
	"context"

	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type Repository interface {
	Create(context.Context, task.CreateInput, task.ActorSnapshot) (task.Task, error)
	Resolve(context.Context, task.Selector, bool) (task.Task, error)
	Update(context.Context, string, int64, task.Patch, task.ActorSnapshot) (task.Task, error)
	Comment(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error)
	Progress(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error)
	Block(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error)
	Unblock(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error)
	Delete(context.Context, string, int64, task.ActorSnapshot) (task.Task, error)
	Restore(context.Context, string, int64, task.ActorSnapshot) (task.Task, error)
	AddDependency(context.Context, string, string, int64, task.ActorSnapshot) (task.Task, error)
	RemoveDependency(context.Context, string, string, int64, task.ActorSnapshot) (task.Task, error)
	List(context.Context, string, task.ListOptions) ([]task.TaskListItem, error)
	Show(context.Context, task.Selector, bool) (task.Show, error)
	Ready(context.Context, string) ([]task.TaskListItem, error)
}

type ActiveRunInspector interface {
	HasActiveRun(context.Context, string) (bool, error)
	TaskRunState(context.Context, string) (run.TaskRunState, error)
}

type NoActiveRuns struct{}

func (NoActiveRuns) HasActiveRun(context.Context, string) (bool, error) {
	return false, nil
}

func (NoActiveRuns) TaskRunState(context.Context, string) (run.TaskRunState, error) {
	return run.TaskRunState{}, nil
}

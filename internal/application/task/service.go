// Package taskapp contains task lifecycle policy independent from transports and storage.
package taskapp

import (
	"context"
	"fmt"

	"s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

type Service struct {
	repository Repository
	runs       ActiveRunInspector
}

func NewService(repository Repository, runs ActiveRunInspector) *Service {
	if runs == nil {
		runs = NoActiveRuns{}
	}

	return &Service{repository: repository, runs: runs}
}

func (s *Service) Create(ctx context.Context, input task.CreateInput, actor task.ActorSnapshot) (task.Task, error) {
	if err := input.Validate(); err != nil {
		return task.Task{}, err
	}
	if err := actor.Validate(); err != nil {
		return task.Task{}, err
	}
	if input.ID == "" {
		id, err := task.NewID()
		if err != nil {
			return task.Task{}, err
		}
		input.ID = id
	}

	return s.repository.Create(ctx, input, actor)
}

func (s *Service) resolve(ctx context.Context, selector task.Selector, deleted bool) (task.Task, error) {
	if err := selector.Validate(); err != nil {
		return task.Task{}, err
	}

	return s.repository.Resolve(ctx, selector, deleted)
}

func requireMutation(expected int64, actor task.ActorSnapshot) error {
	if expected < 1 {
		return task.NewError(task.CodeInvalid, "expected revision is required")
	}

	return actor.Validate()
}

func (s *Service) List(ctx context.Context, projectID string, options task.ListOptions) ([]task.TaskListItem, error) {
	if !task.IsUUIDv7(projectID) {
		return nil, task.NewError(task.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	values, err := s.repository.List(ctx, projectID, options)
	if err != nil {
		return nil, err
	}

	for i := range values {
		state, err := taskRunState(ctx, s.runs, values[i].ID)
		if err != nil {
			return nil, fmt.Errorf("check active run for task %q: %w", values[i].ID, err)
		}
		values[i].HasActiveRun = state.HasActiveRun
		values[i].HasExpiredRun = state.HasExpiredRun
	}

	return values, nil
}

func (s *Service) Show(ctx context.Context, selector task.Selector, deleted bool) (task.Show, error) {
	if err := selector.Validate(); err != nil {
		return task.Show{}, err
	}

	value, err := s.repository.Show(ctx, selector, deleted)
	if err != nil {
		return task.Show{}, err
	}

	state, err := taskRunState(ctx, s.runs, value.Task.ID)
	if err != nil {
		return task.Show{}, fmt.Errorf("check active run for task %q: %w", value.Task.ID, err)
	}

	value.HasActiveRun = state.HasActiveRun
	value.HasExpiredRun = state.HasExpiredRun
	return value, nil
}

func (s *Service) Ready(ctx context.Context, projectID string) ([]task.TaskListItem, error) {
	if !task.IsUUIDv7(projectID) {
		return nil, task.NewError(task.CodeInvalid, "project id must be a canonical UUIDv7")
	}

	values, err := s.repository.Ready(ctx, projectID)
	if err != nil {
		return nil, err
	}

	ready := make([]task.TaskListItem, 0, len(values))
	for _, value := range values {
		state, err := taskRunState(ctx, s.runs, value.ID)
		if err != nil {
			return nil, fmt.Errorf("check active run for task %q: %w", value.ID, err)
		}
		value.HasActiveRun = state.HasActiveRun
		value.HasExpiredRun = state.HasExpiredRun
		if state.HasActiveRun && !state.HasExpiredRun {
			continue
		}

		ready = append(ready, value)
	}

	return ready, nil
}

func taskRunState(ctx context.Context, inspector ActiveRunInspector, taskID string) (run.TaskRunState, error) {
	return inspector.TaskRunState(ctx, taskID)
}

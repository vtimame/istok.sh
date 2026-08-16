// Package taskapp contains task lifecycle policy independent from transports and storage.
package taskapp

import (
	"context"

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

	return s.repository.List(ctx, projectID, options)
}

func (s *Service) Show(ctx context.Context, selector task.Selector, deleted bool) (task.Show, error) {
	if err := selector.Validate(); err != nil {
		return task.Show{}, err
	}

	return s.repository.Show(ctx, selector, deleted)
}

func (s *Service) Ready(ctx context.Context, projectID string) ([]task.TaskListItem, error) {
	if !task.IsUUIDv7(projectID) {
		return nil, task.NewError(task.CodeInvalid, "project id must be a canonical UUIDv7")
	}

	return s.repository.Ready(ctx, projectID)
}

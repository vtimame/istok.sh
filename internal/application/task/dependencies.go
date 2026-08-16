package taskapp

import (
	"context"

	"s26.dev/istok-cli/internal/task"
)

func (s *Service) AddDependency(ctx context.Context, blocker, blocked task.Selector, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	return s.changeDependency(ctx, blocker, blocked, expected, actor, s.repository.AddDependency)
}

func (s *Service) RemoveDependency(ctx context.Context, blocker, blocked task.Selector, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	return s.changeDependency(ctx, blocker, blocked, expected, actor, s.repository.RemoveDependency)
}

func (s *Service) changeDependency(
	ctx context.Context,
	blockerSelector task.Selector,
	blockedSelector task.Selector,
	expected int64,
	actor task.ActorSnapshot,
	mutate func(context.Context, string, string, int64, task.ActorSnapshot) (task.Task, error),
) (task.Task, error) {
	if err := requireMutation(expected, actor); err != nil {
		return task.Task{}, err
	}

	blocker, err := s.resolve(ctx, blockerSelector, true)
	if err != nil {
		return task.Task{}, err
	}
	blocked, err := s.resolve(ctx, blockedSelector, true)
	if err != nil {
		return task.Task{}, err
	}
	if err := task.ValidateDependency(blocker, blocked); err != nil {
		return task.Task{}, err
	}

	return mutate(ctx, blocker.ID, blocked.ID, expected, actor)
}

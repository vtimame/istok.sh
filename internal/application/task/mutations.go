package taskapp

import (
	"context"
	"strings"

	"github.com/vtimame/istok.sh/internal/task"
)

func (s *Service) Update(ctx context.Context, selector task.Selector, expected int64, patch task.Patch, actor task.ActorSnapshot) (task.Task, error) {
	if err := requireMutation(expected, actor); err != nil {
		return task.Task{}, err
	}
	if err := patch.Validate(); err != nil {
		return task.Task{}, err
	}

	value, err := s.resolve(ctx, selector, true)
	if err != nil {
		return task.Task{}, err
	}
	if err := patch.Apply(&value); err != nil {
		return task.Task{}, err
	}

	return s.repository.Update(ctx, value.ID, expected, patch, actor)
}

func (s *Service) Comment(ctx context.Context, selector task.Selector, expected int64, body string, actor task.ActorSnapshot) (task.Task, error) {
	return s.text(ctx, selector, expected, body, actor, s.repository.Comment, func(value *task.Task) error {
		return value.RequireActive()
	})
}

func (s *Service) Progress(ctx context.Context, selector task.Selector, expected int64, body string, actor task.ActorSnapshot) (task.Task, error) {
	return s.text(ctx, selector, expected, body, actor, s.repository.Progress, func(value *task.Task) error {
		return value.RequireActive()
	})
}

func (s *Service) Block(ctx context.Context, selector task.Selector, expected int64, reason string, actor task.ActorSnapshot) (task.Task, error) {
	return s.text(ctx, selector, expected, reason, actor, s.repository.Block, func(value *task.Task) error {
		return value.Block()
	})
}

func (s *Service) Unblock(ctx context.Context, selector task.Selector, expected int64, note string, actor task.ActorSnapshot) (task.Task, error) {
	return s.text(ctx, selector, expected, note, actor, s.repository.Unblock, func(value *task.Task) error {
		return value.Unblock()
	})
}

func (s *Service) text(
	ctx context.Context,
	selector task.Selector,
	expected int64,
	body string,
	actor task.ActorSnapshot,
	mutate func(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error),
	validate func(*task.Task) error,
) (task.Task, error) {
	if err := requireMutation(expected, actor); err != nil {
		return task.Task{}, err
	}
	if strings.TrimSpace(body) == "" {
		return task.Task{}, task.NewError(task.CodeInvalid, "event body is required")
	}

	value, err := s.resolve(ctx, selector, true)
	if err != nil {
		return task.Task{}, err
	}
	if err := validate(&value); err != nil {
		return task.Task{}, err
	}

	return mutate(ctx, value.ID, expected, body, actor)
}

func (s *Service) Delete(ctx context.Context, selector task.Selector, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	if err := requireMutation(expected, actor); err != nil {
		return task.Task{}, err
	}

	value, err := s.resolve(ctx, selector, true)
	if err != nil {
		return task.Task{}, err
	}
	if value.DeletedAt == nil {
		active, err := s.runs.HasActiveRun(ctx, value.ID)
		if err != nil {
			return task.Task{}, err
		}
		if active {
			return task.Task{}, task.NewError(task.CodeHasActiveRun, "task has an active run")
		}
	}

	return s.repository.Delete(ctx, value.ID, expected, actor)
}

func (s *Service) Restore(ctx context.Context, selector task.Selector, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	if err := requireMutation(expected, actor); err != nil {
		return task.Task{}, err
	}

	value, err := s.resolve(ctx, selector, true)
	if err != nil {
		return task.Task{}, err
	}

	return s.repository.Restore(ctx, value.ID, expected, actor)
}

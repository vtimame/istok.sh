package taskapp

import (
	"context"
	"errors"
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

// Comment adds a comment. An expected revision of 0 appends without
// compare-and-swap: a comment only adds history, so it never conflicts with
// another writer.
func (s *Service) Comment(ctx context.Context, selector task.Selector, expected int64, body string, actor task.ActorSnapshot) (task.Task, error) {
	return s.appendText(ctx, selector, expected, body, actor, s.repository.Comment)
}

// Progress adds a progress note. An expected revision of 0 appends without
// compare-and-swap, as for Comment.
func (s *Service) Progress(ctx context.Context, selector task.Selector, expected int64, body string, actor task.ActorSnapshot) (task.Task, error) {
	return s.appendText(ctx, selector, expected, body, actor, s.repository.Progress)
}

// Handoff records where the work stands for the next session: what is done,
// where it stopped, decisions and the next step. The latest handoff is the
// current one. It appends without compare-and-swap, as for Comment.
func (s *Service) Handoff(ctx context.Context, selector task.Selector, body string, actor task.ActorSnapshot) (task.Task, error) {
	return s.appendText(ctx, selector, 0, body, actor, s.repository.Handoff)
}

// appendAttempts bounds retries when another writer changes the task between
// reading its revision and appending the event.
const appendAttempts = 5

func (s *Service) appendText(
	ctx context.Context,
	selector task.Selector,
	expected int64,
	body string,
	actor task.ActorSnapshot,
	mutate func(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error),
) (task.Task, error) {
	requireActive := func(value *task.Task) error { return value.RequireActive() }

	if expected != 0 {
		return s.text(ctx, selector, expected, body, actor, mutate, requireActive)
	}

	var lastErr error
	for range appendAttempts {
		current, err := s.resolve(ctx, selector, true)
		if err != nil {
			return task.Task{}, err
		}

		value, err := s.text(ctx, selector, current.Revision, body, actor, mutate, requireActive)
		if !isRevisionConflict(err) {
			return value, err
		}
		lastErr = err
	}

	return task.Task{}, lastErr
}

func isRevisionConflict(err error) bool {
	var taskErr *task.Error

	return errors.As(err, &taskErr) && taskErr.Code == task.CodeRevisionConflict
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

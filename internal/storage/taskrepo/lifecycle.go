package taskrepo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/vtimame/istok.sh/internal/task"
)

func (r *Repository) Update(ctx context.Context, id string, expected int64, patch task.Patch, actor task.ActorSnapshot) (task.Task, error) {
	if !task.IsUUIDv7(id) || expected < 1 {
		return task.Task{}, task.NewError(task.CodeInvalid, "task id and expected revision are required")
	}
	if err := patch.Validate(); err != nil {
		return task.Task{}, err
	}
	if err := actor.Validate(); err != nil {
		return task.Task{}, err
	}

	return r.mutate(ctx, id, expected, actor, "updated", "", patch.Apply)
}

func (r *Repository) Comment(ctx context.Context, id string, expected int64, body string, actor task.ActorSnapshot) (task.Task, error) {
	return r.textMutation(ctx, id, expected, body, actor, "commented", false, false)
}

func (r *Repository) Progress(ctx context.Context, id string, expected int64, body string, actor task.ActorSnapshot) (task.Task, error) {
	return r.textMutation(ctx, id, expected, body, actor, "progress", false, false)
}

func (r *Repository) Block(ctx context.Context, id string, expected int64, reason string, actor task.ActorSnapshot) (task.Task, error) {
	return r.textMutation(ctx, id, expected, reason, actor, "blocked", true, false)
}

func (r *Repository) Unblock(ctx context.Context, id string, expected int64, note string, actor task.ActorSnapshot) (task.Task, error) {
	return r.textMutation(ctx, id, expected, note, actor, "unblocked", false, true)
}

func (r *Repository) textMutation(ctx context.Context, id string, expected int64, body string, actor task.ActorSnapshot, event string, block, unblock bool) (task.Task, error) {
	if strings.TrimSpace(body) == "" {
		return task.Task{}, task.NewError(task.CodeInvalid, "event body is required")
	}
	if !task.IsUUIDv7(id) || expected < 1 {
		return task.Task{}, task.NewError(task.CodeInvalid, "task id and expected revision are required")
	}
	if err := actor.Validate(); err != nil {
		return task.Task{}, err
	}

	return r.mutate(ctx, id, expected, actor, event, body, func(v *task.Task) error {
		if block {
			return v.Block()
		}
		if unblock {
			return v.Unblock()
		}

		return v.RequireActive()
	})
}

func (r *Repository) mutate(ctx context.Context, id string, expected int64, actor task.ActorSnapshot, event, body string, change func(*task.Task) error) (task.Task, error) {
	return r.write(ctx, func(c *sql.Conn) (task.Task, error) {
		v, err := getTask(ctx, c, id)
		if err != nil {
			return task.Task{}, err
		}
		if v.Revision != expected {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}
		if err = change(&v); err != nil {
			return task.Task{}, err
		}

		now := time.Now().UTC()
		result, err := c.ExecContext(ctx, `UPDATE tasks SET status=?,title=?,description=?,acceptance_criteria=?,notes=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, v.Status, v.Title, v.Description, v.AcceptanceCriteria, v.Notes, stamp(now), id, expected)
		if err != nil {
			return task.Task{}, fmt.Errorf("update task: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return task.Task{}, fmt.Errorf("check task update: %w", err)
		}
		if changed != 1 {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}

		v, err = getTask(ctx, c, id)
		if err != nil {
			return task.Task{}, err
		}
		if err = insertEvent(ctx, c, id, event, body, v.Revision, actor, now); err != nil {
			return task.Task{}, err
		}

		return v, nil
	})
}

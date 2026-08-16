package taskrepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"s26.dev/istok-cli/internal/task"
)

func (r *Repository) Delete(ctx context.Context, id string, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	return r.changeDeleted(ctx, id, expected, actor, true)
}

func (r *Repository) Restore(ctx context.Context, id string, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	return r.changeDeleted(ctx, id, expected, actor, false)
}

func (r *Repository) changeDeleted(ctx context.Context, id string, expected int64, actor task.ActorSnapshot, deleted bool) (task.Task, error) {
	if !task.IsUUIDv7(id) || expected < 1 {
		return task.Task{}, task.NewError(task.CodeInvalid, "task id and expected revision are required")
	}

	if err := actor.Validate(); err != nil {
		return task.Task{}, err
	}

	return r.write(ctx, func(conn *sql.Conn) (task.Task, error) {
		value, err := getTask(ctx, conn, id)
		if err != nil {
			return task.Task{}, err
		}
		if (value.DeletedAt != nil) == deleted {
			return value, nil
		}
		if value.Revision != expected {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}

		now := time.Now().UTC()
		result, err := updateDeleted(ctx, conn, id, expected, deleted, now)
		if err != nil {
			return task.Task{}, err
		}

		changed, err := result.RowsAffected()
		if err != nil {
			return task.Task{}, fmt.Errorf("check task state update: %w", err)
		}
		if changed != 1 {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}

		value, err = getTask(ctx, conn, id)
		if err != nil {
			return task.Task{}, err
		}

		eventType := "restored"
		if deleted {
			eventType = "deleted"
		}

		if err := insertEvent(ctx, conn, id, eventType, "", value.Revision, actor, now); err != nil {
			return task.Task{}, err
		}

		return value, nil
	})
}

func updateDeleted(ctx context.Context, conn *sql.Conn, id string, expected int64, deleted bool, now time.Time) (sql.Result, error) {
	if deleted {
		result, err := conn.ExecContext(ctx, `UPDATE tasks SET deleted_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND deleted_at IS NULL`, stamp(now), stamp(now), id, expected)
		if err != nil {
			return nil, fmt.Errorf("mark task deleted: %w", err)
		}

		return result, nil
	}

	result, err := conn.ExecContext(ctx, `UPDATE tasks SET deleted_at=NULL,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND deleted_at IS NOT NULL`, stamp(now), id, expected)
	if err != nil {
		return nil, fmt.Errorf("restore task: %w", err)
	}

	return result, nil
}

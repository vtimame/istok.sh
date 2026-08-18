package taskrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vtimame/istok.sh/internal/task"
)

func (r *Repository) AddDependency(ctx context.Context, blockerID, blockedID string, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	if !task.IsUUIDv7(blockerID) || !task.IsUUIDv7(blockedID) || expected < 1 {
		return task.Task{}, task.NewError(task.CodeInvalid, "task ids and expected revision are required")
	}
	if err := actor.Validate(); err != nil {
		return task.Task{}, err
	}

	return r.write(ctx, func(c *sql.Conn) (task.Task, error) {
		blocker, err := getTask(ctx, c, blockerID)
		if err != nil {
			return task.Task{}, err
		}
		blocked, err := getTask(ctx, c, blockedID)
		if err != nil {
			return task.Task{}, err
		}
		if err := task.ValidateDependency(blocker, blocked); err != nil {
			return task.Task{}, err
		}
		if blocked.Revision != expected {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}

		var exists int
		err = c.QueryRowContext(ctx, `SELECT 1 FROM task_dependencies WHERE project_id=? AND blocker_task_id=? AND blocked_task_id=? AND edge_type='blocks'`, blocked.ProjectID, blockerID, blockedID).Scan(&exists)
		if err == nil {
			return task.Task{}, task.NewError(task.CodeConflict, "dependency already exists")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return task.Task{}, fmt.Errorf("check dependency: %w", err)
		}

		var cycle int
		err = c.QueryRowContext(ctx, `WITH RECURSIVE reach(id) AS (SELECT blocked_task_id FROM task_dependencies WHERE blocker_task_id=? UNION SELECT d.blocked_task_id FROM task_dependencies d JOIN reach r ON d.blocker_task_id=r.id) SELECT 1 FROM reach WHERE id=? LIMIT 1`, blockedID, blockerID).Scan(&cycle)
		if err == nil {
			return task.Task{}, task.NewError(task.CodeDependencyCycle, "dependency would create a cycle")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return task.Task{}, fmt.Errorf("detect dependency cycle: %w", err)
		}

		dependencyID, err := task.NewID()
		if err != nil {
			return task.Task{}, fmt.Errorf("generate dependency ID: %w", err)
		}

		now := time.Now().UTC()
		if _, err = c.ExecContext(ctx, `INSERT INTO task_dependencies(id,project_id,blocker_task_id,blocked_task_id,edge_type,created_at) VALUES(?,?,?,?, 'blocks',?)`, dependencyID, blocked.ProjectID, blockerID, blockedID, stamp(now)); err != nil {
			return task.Task{}, fmt.Errorf("insert dependency: %w", mapSQLError(err))
		}

		result, err := c.ExecContext(ctx, `UPDATE tasks SET revision=revision+1,updated_at=? WHERE id=? AND revision=?`, stamp(now), blockedID, expected)
		if err != nil {
			return task.Task{}, fmt.Errorf("increment dependency task revision: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return task.Task{}, fmt.Errorf("check dependency task revision: %w", err)
		}
		if changed != 1 {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}

		blocked, err = getTask(ctx, c, blockedID)
		if err != nil {
			return task.Task{}, err
		}
		if err = insertEvent(ctx, c, blockedID, "dependency_added", blockerID, blocked.Revision, actor, now); err != nil {
			return task.Task{}, err
		}

		return blocked, nil
	})
}

func (r *Repository) RemoveDependency(ctx context.Context, blockerID, blockedID string, expected int64, actor task.ActorSnapshot) (task.Task, error) {
	if !task.IsUUIDv7(blockerID) || !task.IsUUIDv7(blockedID) || expected < 1 {
		return task.Task{}, task.NewError(task.CodeInvalid, "task ids and expected revision are required")
	}
	if err := actor.Validate(); err != nil {
		return task.Task{}, err
	}

	return r.write(ctx, func(c *sql.Conn) (task.Task, error) {
		blocker, err := getTask(ctx, c, blockerID)
		if err != nil {
			return task.Task{}, err
		}
		blocked, err := getTask(ctx, c, blockedID)
		if err != nil {
			return task.Task{}, err
		}
		if err := task.ValidateDependency(blocker, blocked); err != nil {
			return task.Task{}, err
		}
		if blocked.Revision != expected {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}

		result, err := c.ExecContext(ctx, `DELETE FROM task_dependencies WHERE project_id=? AND blocker_task_id=? AND blocked_task_id=? AND edge_type='blocks'`, blocked.ProjectID, blockerID, blockedID)
		if err != nil {
			return task.Task{}, fmt.Errorf("remove dependency: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return task.Task{}, err
		}
		if n != 1 {
			return task.Task{}, task.NewError(task.CodeNotFound, "dependency was not found")
		}

		now := time.Now().UTC()
		increment, err := c.ExecContext(ctx, `UPDATE tasks SET revision=revision+1,updated_at=? WHERE id=? AND revision=?`, stamp(now), blockedID, expected)
		if err != nil {
			return task.Task{}, fmt.Errorf("increment dependency task revision: %w", err)
		}
		changed, err := increment.RowsAffected()
		if err != nil {
			return task.Task{}, fmt.Errorf("check dependency task revision: %w", err)
		}
		if changed != 1 {
			return task.Task{}, task.NewError(task.CodeRevisionConflict, "task revision does not match expected_revision")
		}

		blocked, err = getTask(ctx, c, blockedID)
		if err != nil {
			return task.Task{}, err
		}
		if err = insertEvent(ctx, c, blockedID, "dependency_removed", blockerID, blocked.Revision, actor, now); err != nil {
			return task.Task{}, err
		}

		return blocked, nil
	})
}

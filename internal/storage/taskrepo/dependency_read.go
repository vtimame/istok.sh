package taskrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/vtimame/istok.sh/internal/task"
)

func (r *Repository) Dependencies(ctx context.Context, taskID string, incoming bool) ([]task.Dependency, error) {
	if !task.IsUUIDv7(taskID) {
		return nil, task.NewError(task.CodeInvalid, "task id must be a canonical UUIDv7")
	}

	column := "blocked_task_id"
	if !incoming {
		column = "blocker_task_id"
	}

	rows, err := r.db.QueryContext(ctx, `SELECT id,project_id,blocker_task_id,blocked_task_id,edge_type,created_at FROM task_dependencies WHERE `+column+`=? ORDER BY created_at,id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list dependencies: %w", err)
	}
	defer rows.Close()

	var values []task.Dependency
	for rows.Next() {
		value, err := scanDependency(rows)
		if err != nil {
			return nil, err
		}

		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dependencies: %w", err)
	}

	return values, nil
}

func scanDependency(row scanner) (task.Dependency, error) {
	var value task.Dependency
	var created string

	if err := row.Scan(&value.ID, &value.ProjectID, &value.BlockerTaskID, &value.BlockedTaskID, &value.EdgeType, &created); err != nil {
		return value, fmt.Errorf("scan dependency: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return value, fmt.Errorf("parse dependency time: %w", err)
	}

	value.CreatedAt = createdAt
	return value, nil
}

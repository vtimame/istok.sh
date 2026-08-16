package taskrepo

import (
	"context"
	"fmt"

	"s26.dev/istok-cli/internal/task"
)

func (r *Repository) Ready(ctx context.Context, projectID string) ([]task.TaskListItem, error) {
	if !task.IsUUIDv7(projectID) {
		return nil, task.NewError(task.CodeInvalid, "project id must be a canonical UUIDv7")
	}

	rows, err := r.db.QueryContext(ctx, taskQuery+`t WHERE t.project_id=? AND t.deleted_at IS NULL AND t.status='open' AND NOT EXISTS (SELECT 1 FROM task_dependencies d JOIN tasks blocker ON blocker.id=d.blocker_task_id WHERE d.blocked_task_id=t.id AND blocker.deleted_at IS NULL AND blocker.status != 'done') ORDER BY t.number`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list ready tasks: %w", err)
	}
	defer rows.Close()

	var values []task.TaskListItem
	for rows.Next() {
		value, err := scanTask(rows)
		if err != nil {
			return nil, err
		}

		values = append(values, task.TaskListItem{Task: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ready tasks: %w", err)
	}

	return values, nil
}

func (r *Repository) Show(ctx context.Context, selector task.Selector, includeDeleted bool) (task.Show, error) {
	value, err := r.Resolve(ctx, selector, includeDeleted)
	if err != nil {
		return task.Show{}, err
	}

	events, err := r.Events(ctx, value.ID)
	if err != nil {
		return task.Show{}, err
	}
	incoming, err := r.Dependencies(ctx, value.ID, true)
	if err != nil {
		return task.Show{}, err
	}
	outgoing, err := r.Dependencies(ctx, value.ID, false)
	if err != nil {
		return task.Show{}, err
	}

	return task.Show{
		Task:     value,
		Events:   events,
		Incoming: incoming,
		Outgoing: outgoing,
	}, nil
}

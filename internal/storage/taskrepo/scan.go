package taskrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"s26.dev/istok-cli/internal/task"
)

const taskQuery = `SELECT id,project_id,number,revision,status,title,description,acceptance_criteria,notes,created_at,updated_at,deleted_at FROM tasks `

type scanner interface {
	Scan(...any) error
}

func (r *Repository) Get(ctx context.Context, id string, includeDeleted bool) (task.Task, error) {
	if !task.IsUUIDv7(id) {
		return task.Task{}, task.NewError(task.CodeInvalid, "task id must be a canonical UUIDv7")
	}

	where := "WHERE id=?"
	if !includeDeleted {
		where += " AND deleted_at IS NULL"
	}

	return scanTask(r.db.QueryRowContext(ctx, taskQuery+where, id))
}

func (r *Repository) Resolve(ctx context.Context, selector task.Selector, includeDeleted bool) (task.Task, error) {
	if err := selector.Validate(); err != nil {
		return task.Task{}, err
	}

	where := "WHERE project_id=?"
	arg := []any{selector.ProjectID}
	if selector.ID != "" {
		where += " AND id=?"
		arg = append(arg, selector.ID)
	} else {
		where += " AND number=?"
		arg = append(arg, selector.Number)
	}
	if !includeDeleted {
		where += " AND deleted_at IS NULL"
	}

	return scanTask(r.db.QueryRowContext(ctx, taskQuery+where, arg...))
}

func (r *Repository) List(ctx context.Context, projectID string, options task.ListOptions) ([]task.TaskListItem, error) {
	if !task.IsUUIDv7(projectID) {
		return nil, task.NewError(task.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	where := " WHERE t.project_id=?"
	args := []any{projectID}
	if !options.IncludeDeleted {
		where += " AND t.deleted_at IS NULL"
	}
	if len(options.Statuses) > 0 {
		where += " AND t.status IN ("
		for i, status := range options.Statuses {
			if i > 0 {
				where += ","
			}
			where += "?"
			args = append(args, status)
		}
		where += ")"
	}

	rows, err := r.db.QueryContext(ctx, taskQuery+"t"+where+" ORDER BY t.number", args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var values []task.Task
	for rows.Next() {
		value, err := scanTask(rows)
		if err != nil {
			return nil, err
		}

		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close task list: %w", err)
	}

	result := make([]task.TaskListItem, 0, len(values))
	for _, value := range values {
		blockers, err := r.activeBlockers(ctx, value.ID)
		if err != nil {
			return nil, err
		}

		result = append(result, task.TaskListItem{Task: value, ActiveBlockers: blockers})
	}

	return result, nil
}

func (r *Repository) activeBlockers(ctx context.Context, taskID string) ([]task.BlockerSummary, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT t.id,t.number,t.status,t.title FROM task_dependencies d JOIN tasks t ON t.id=d.blocker_task_id WHERE d.blocked_task_id=? AND t.deleted_at IS NULL AND t.status != 'done' ORDER BY t.number`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list active blockers: %w", err)
	}
	defer rows.Close()

	var result []task.BlockerSummary
	for rows.Next() {
		var v task.BlockerSummary
		if err := rows.Scan(&v.ID, &v.Number, &v.Status, &v.Title); err != nil {
			return nil, fmt.Errorf("scan active blocker: %w", err)
		}

		result = append(result, v)
	}

	return result, rows.Err()
}

func getTask(ctx context.Context, conn *sql.Conn, id string) (task.Task, error) {
	return scanTask(conn.QueryRowContext(ctx, taskQuery+`WHERE id=?`, id))
}

func scanTask(row scanner) (task.Task, error) {
	var value task.Task
	var created, updated string
	var deleted sql.NullString

	err := row.Scan(&value.ID, &value.ProjectID, &value.Number, &value.Revision, &value.Status, &value.Title, &value.Description, &value.AcceptanceCriteria, &value.Notes, &created, &updated, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, task.NewError(task.CodeNotFound, "task was not found")
	}
	if err != nil {
		return task.Task{}, fmt.Errorf("scan task: %w", err)
	}

	var parseErr error
	value.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
	if parseErr != nil {
		return task.Task{}, fmt.Errorf("parse task creation time: %w", parseErr)
	}

	value.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated)
	if parseErr != nil {
		return task.Task{}, fmt.Errorf("parse task update time: %w", parseErr)
	}

	if deleted.Valid {
		value.DeletedAt, parseErr = parseTime(deleted.String)
		if parseErr != nil {
			return task.Task{}, fmt.Errorf("parse task deletion time: %w", parseErr)
		}
	}

	return value, nil
}

func parseTime(value string) (*time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func stamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

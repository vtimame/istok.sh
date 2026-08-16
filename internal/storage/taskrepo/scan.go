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

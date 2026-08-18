package taskrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vtimame/istok.sh/internal/task"
)

func (r *Repository) Create(ctx context.Context, input task.CreateInput, actor task.ActorSnapshot) (task.Task, error) {
	if err := input.Validate(); err != nil {
		return task.Task{}, err
	}

	if err := actor.Validate(); err != nil {
		return task.Task{}, err
	}

	if input.ID == "" {
		id, err := task.NewID()
		if err != nil {
			return task.Task{}, fmt.Errorf("generate task ID: %w", err)
		}

		input.ID = id
	}

	value := task.Task{
		ID:                 input.ID,
		ProjectID:          input.ProjectID,
		Status:             task.StatusOpen,
		Title:              strings.TrimSpace(input.Title),
		Description:        input.Description,
		AcceptanceCriteria: input.AcceptanceCriteria,
		Notes:              input.Notes,
	}

	return r.write(ctx, func(conn *sql.Conn) (task.Task, error) {
		if err := requireActiveProject(ctx, conn, value.ProjectID); err != nil {
			return task.Task{}, err
		}

		number, err := allocateNumber(ctx, conn, value.ProjectID)
		if err != nil {
			return task.Task{}, err
		}

		now := time.Now().UTC()
		value.Number = number
		value.Revision = 1
		value.CreatedAt = now
		value.UpdatedAt = now

		_, err = conn.ExecContext(ctx, `INSERT INTO tasks(id,project_id,number,revision,status,title,description,acceptance_criteria,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, value.ID, value.ProjectID, value.Number, value.Revision, value.Status, value.Title, value.Description, value.AcceptanceCriteria, value.Notes, stamp(now), stamp(now))
		if err != nil {
			return task.Task{}, fmt.Errorf("insert task: %w", mapSQLError(err))
		}

		if err := insertEvent(ctx, conn, value.ID, "created", "", value.Revision, actor, now); err != nil {
			return task.Task{}, err
		}

		return value, nil
	})
}

func requireActiveProject(ctx context.Context, conn *sql.Conn, projectID string) error {
	var deleted sql.NullString
	err := conn.QueryRowContext(ctx, `SELECT deleted_at FROM projects WHERE id=?`, projectID).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return task.NewError(task.CodeConflict, "project was not found")
	}
	if err != nil {
		return fmt.Errorf("get project for task creation: %w", err)
	}
	if deleted.Valid {
		return task.NewError(task.CodeConflict, "cannot create a task in a deleted project")
	}

	return nil
}

func allocateNumber(ctx context.Context, conn *sql.Conn, projectID string) (int64, error) {
	_, err := conn.ExecContext(ctx, `INSERT INTO project_task_counters(project_id,next_number) VALUES(?,1) ON CONFLICT(project_id) DO NOTHING`, projectID)
	if err != nil {
		return 0, fmt.Errorf("initialize task counter: %w", mapSQLError(err))
	}

	var number int64
	if err := conn.QueryRowContext(ctx, `SELECT next_number FROM project_task_counters WHERE project_id=?`, projectID).Scan(&number); err != nil {
		return 0, fmt.Errorf("read task counter: %w", err)
	}

	_, err = conn.ExecContext(ctx, `UPDATE project_task_counters SET next_number=next_number+1 WHERE project_id=?`, projectID)
	if err != nil {
		return 0, fmt.Errorf("advance task counter: %w", err)
	}

	return number, nil
}

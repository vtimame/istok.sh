package taskrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vtimame/istok.sh/internal/task"
)

func (r *Repository) Events(ctx context.Context, id string) ([]task.Event, error) {
	if !task.IsUUIDv7(id) {
		return nil, task.NewError(task.CodeInvalid, "task id must be a canonical UUIDv7")
	}

	if err := r.requireTask(ctx, id); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `SELECT id,task_id,type,body,task_revision,actor_id,actor_kind,actor_name,created_at FROM task_events WHERE task_id=? ORDER BY created_at,id`, id)
	if err != nil {
		return nil, fmt.Errorf("list task events: %w", err)
	}
	defer rows.Close()

	var values []task.Event
	for rows.Next() {
		value, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}

		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task events: %w", err)
	}

	return values, nil
}

func (r *Repository) requireTask(ctx context.Context, id string) error {
	var exists int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id=?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return task.NewError(task.CodeNotFound, "task was not found")
	}
	if err != nil {
		return fmt.Errorf("check task for events: %w", err)
	}

	return nil
}

func insertEvent(ctx context.Context, conn *sql.Conn, taskID, kind, body string, revision int64, actor task.ActorSnapshot, now time.Time) error {
	id, err := task.NewID()
	if err != nil {
		return fmt.Errorf("generate task event ID: %w", err)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO task_events(id,task_id,type,body,task_revision,actor_id,actor_kind,actor_name,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, taskID, kind, body, revision, actor.ID, actor.Kind, actor.Name, stamp(now))
	if err != nil {
		return fmt.Errorf("insert task event: %w", mapSQLError(err))
	}

	return nil
}

func scanEvent(row scanner) (task.Event, error) {
	var value task.Event
	var created string

	err := row.Scan(&value.ID, &value.TaskID, &value.Type, &value.Body, &value.TaskRevision, &value.Actor.ID, &value.Actor.Kind, &value.Actor.Name, &created)
	if err != nil {
		return task.Event{}, fmt.Errorf("scan task event: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return task.Event{}, fmt.Errorf("parse task event time: %w", err)
	}

	value.CreatedAt = createdAt
	return value, nil
}

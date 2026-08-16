package taskrepo

import (
	"context"
	"database/sql"
	"fmt"

	"s26.dev/istok-cli/internal/task"
)

func (r *Repository) write(ctx context.Context, fn func(*sql.Conn) (task.Task, error)) (task.Task, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return task.Task{}, fmt.Errorf("acquire task write connection: %w", err)
	}
	defer conn.Close()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return task.Task{}, fmt.Errorf("begin task write transaction: %w", err)
	}

	done := false
	defer func() {
		if !done {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	value, err := fn(conn)
	if err != nil {
		return task.Task{}, err
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return task.Task{}, fmt.Errorf("commit task write transaction: %w", err)
	}

	done = true
	return value, nil
}

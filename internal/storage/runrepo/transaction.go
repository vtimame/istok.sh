package runrepo

import (
	"context"
	"database/sql"
	"fmt"
)

func (r *Repository) write(ctx context.Context, operation string, fn func(*sql.Conn) error) error {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire %s connection: %w", operation, err)
	}
	defer conn.Close()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin %s transaction: %w", operation, err)
	}

	done := false
	defer func() {
		if !done {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	if err := fn(conn); err != nil {
		return err
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit %s transaction: %w", operation, err)
	}

	done = true
	return nil
}

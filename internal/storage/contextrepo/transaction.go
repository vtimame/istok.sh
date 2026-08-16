package contextrepo

import (
	"context"
	"database/sql"
	"fmt"

	contextmodel "s26.dev/istok-cli/internal/context"
)

func (r *Repository) write(ctx context.Context, fn func(*sql.Conn) (contextmodel.ProjectContextRecord, error)) (contextmodel.ProjectContextRecord, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return contextmodel.ProjectContextRecord{}, fmt.Errorf("acquire context write connection: %w", err)
	}
	defer conn.Close()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return contextmodel.ProjectContextRecord{}, fmt.Errorf("begin context write transaction: %w", err)
	}

	done := false
	defer func() {
		if !done {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	value, err := fn(conn)
	if err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return contextmodel.ProjectContextRecord{}, fmt.Errorf("commit context write transaction: %w", err)
	}

	done = true
	return value, nil
}

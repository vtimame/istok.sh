package taskrepo

import (
	"context"
	"fmt"
	"time"
)

// LastActivity returns, per project, the latest update time of its live tasks.
// The web UI uses it to order projects by recent work.
func (r *Repository) LastActivity(ctx context.Context) (map[string]time.Time, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT project_id, MAX(updated_at) FROM tasks WHERE deleted_at IS NULL GROUP BY project_id`)
	if err != nil {
		return nil, fmt.Errorf("query task activity: %w", err)
	}
	defer rows.Close()

	activity := make(map[string]time.Time)
	for rows.Next() {
		var projectID, updated string
		if err := rows.Scan(&projectID, &updated); err != nil {
			return nil, fmt.Errorf("scan task activity: %w", err)
		}

		updatedAt, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse task activity time: %w", err)
		}
		activity[projectID] = updatedAt
	}

	return activity, rows.Err()
}

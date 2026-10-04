package taskrepo

import (
	"context"
	"fmt"
	"time"
)

// ProjectStats is a per-project summary of live tasks and runs for the web UI.
type ProjectStats struct {
	Open           int        `json:"open"`
	Blocked        int        `json:"blocked"`
	Done           int        `json:"done"`
	ActiveRuns     int        `json:"active_runs"`
	StaleRuns      int        `json:"stale_runs"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
}

// ProjectStats counts live tasks by status and active runs per project; an
// active run whose lease has expired counts as stale. Last activity is the
// latest task or run update, so a long-running agent that only heartbeats
// still moves its project up.
func (r *Repository) ProjectStats(ctx context.Context) (map[string]ProjectStats, error) {
	stats := make(map[string]ProjectStats)
	now := time.Now().UTC()

	taskRows, err := r.db.QueryContext(ctx, `SELECT project_id,
		SUM(status = 'open'), SUM(status = 'blocked'), SUM(status = 'done'), MAX(updated_at)
		FROM tasks WHERE deleted_at IS NULL GROUP BY project_id`)
	if err != nil {
		return nil, fmt.Errorf("query task stats: %w", err)
	}
	defer taskRows.Close()

	for taskRows.Next() {
		var projectID, updated string
		var value ProjectStats
		if err := taskRows.Scan(&projectID, &value.Open, &value.Blocked, &value.Done, &updated); err != nil {
			return nil, fmt.Errorf("scan task stats: %w", err)
		}

		updatedAt, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse task activity time: %w", err)
		}
		value.LastActivityAt = &updatedAt
		stats[projectID] = value
	}
	if err := taskRows.Err(); err != nil {
		return nil, err
	}

	runRows, err := r.db.QueryContext(ctx, `SELECT t.project_id, r.status, r.updated_at, COALESCE(l.expires_at, r.updated_at)
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		LEFT JOIN run_leases l ON l.run_id = r.id
		WHERE t.deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("query run stats: %w", err)
	}
	defer runRows.Close()

	for runRows.Next() {
		var projectID, status, updated, expires string
		if err := runRows.Scan(&projectID, &status, &updated, &expires); err != nil {
			return nil, fmt.Errorf("scan run stats: %w", err)
		}

		updatedAt, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse run activity time: %w", err)
		}

		expiresAt, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			return nil, fmt.Errorf("parse run lease expiry: %w", err)
		}

		value := stats[projectID]
		if status == "active" && expiresAt.After(now) {
			value.ActiveRuns++
		} else if status == "active" {
			value.StaleRuns++
		}
		if value.LastActivityAt == nil || updatedAt.After(*value.LastActivityAt) {
			value.LastActivityAt = &updatedAt
		}
		stats[projectID] = value
	}

	return stats, runRows.Err()
}

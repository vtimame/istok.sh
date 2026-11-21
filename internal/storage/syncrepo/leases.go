package syncrepo

import (
	"context"
	"database/sql"
	"time"
)

// HeldLease is a run claimed on this device whose task lease it keeps on the
// hub. Active is false once the run ended or vanished; Expires is the local
// lease expiry of an active run.
type HeldLease struct {
	TaskID  string
	RunID   string
	Active  bool
	Expires time.Time
}

// HoldLease remembers a run claimed on this device. A project that does not
// sync is ignored.
func (r *Repository) HoldLease(ctx context.Context, projectID, taskID, runID string) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO sync_leases (project_id, task_id, run_id) SELECT project_id, ?, ? FROM sync_projects WHERE project_id = ? "+
			"ON CONFLICT(project_id, task_id) DO UPDATE SET run_id = excluded.run_id", taskID, runID, projectID)

	return err
}

// DropLease forgets a held lease unless another run took the task since.
func (r *Repository) DropLease(ctx context.Context, projectID, taskID, runID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM sync_leases WHERE project_id = ? AND task_id = ? AND run_id = ?",
		projectID, taskID, runID)

	return err
}

// HeldLeases lists the leases this device keeps for a project with the state
// of their runs.
func (r *Repository) HeldLeases(ctx context.Context, projectID string) ([]HeldLease, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT h.task_id, h.run_id, COALESCE(r.status, ''), COALESCE(l.expires_at, '') FROM sync_leases h "+
			"LEFT JOIN runs r ON r.id = h.run_id LEFT JOIN run_leases l ON l.run_id = h.run_id "+
			"WHERE h.project_id = ? ORDER BY h.task_id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HeldLease
	for rows.Next() {
		var value HeldLease
		var status, expires string
		if err := rows.Scan(&value.TaskID, &value.RunID, &status, &expires); err != nil {
			return nil, err
		}

		if parsed, err := time.Parse(time.RFC3339Nano, expires); err == nil && status == "active" {
			value.Active, value.Expires = true, parsed
		}
		result = append(result, value)
	}

	return result, rows.Err()
}

// Synced reports whether the device syncs a project.
func (r *Repository) Synced(ctx context.Context, projectID string) (bool, error) {
	var found int
	err := r.db.QueryRowContext(ctx, "SELECT 1 FROM sync_projects WHERE project_id = ?", projectID).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}

	return err == nil, err
}

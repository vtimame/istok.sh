// Package syncrepo stores this device's hub sync state: which projects sync,
// the hub cursor, and what the hub holds for each row.
package syncrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Project is a project this device syncs.
type Project struct {
	ID       string
	Cursor   int64
	Enabled  time.Time
	SyncedAt *time.Time
}

// RowState is what the hub holds for a row and the local tag at that moment.
type RowState struct {
	Revision int64
	HubTag   string
	LocalTag string
}

// RowKey names a row within a project.
type RowKey struct {
	Table string
	Key   string
}

// Update records one row after a pull or a push.
type Update struct {
	RowKey
	RowState
}

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository { return &Repository{db: db} }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Enable starts syncing a project; enabling twice keeps the cursor.
func (r *Repository) Enable(ctx context.Context, projectID string) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO sync_projects (project_id, enabled_at) VALUES (?, ?) ON CONFLICT(project_id) DO NOTHING", projectID, now())

	return err
}

// Disable stops syncing a project and forgets its row state.
func (r *Repository) Disable(ctx context.Context, projectID string) (bool, error) {
	result, err := r.db.ExecContext(ctx, "DELETE FROM sync_projects WHERE project_id = ?", projectID)
	if err != nil {
		return false, err
	}
	removed, _ := result.RowsAffected()

	return removed > 0, nil
}

// Reset forgets every project's state; used when the device joins another hub.
func (r *Repository) Reset(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM sync_projects")

	return err
}

func (r *Repository) Projects(ctx context.Context) ([]Project, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT project_id, cursor, enabled_at, synced_at FROM sync_projects ORDER BY enabled_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Project
	for rows.Next() {
		var value Project
		var enabled string
		var synced sql.NullString
		if err := rows.Scan(&value.ID, &value.Cursor, &enabled, &synced); err != nil {
			return nil, err
		}

		value.Enabled, _ = time.Parse(time.RFC3339Nano, enabled)
		if synced.Valid {
			parsed, err := time.Parse(time.RFC3339Nano, synced.String)
			if err == nil {
				value.SyncedAt = &parsed
			}
		}
		result = append(result, value)
	}

	return result, rows.Err()
}

func (r *Repository) Project(ctx context.Context, projectID string) (Project, bool, error) {
	projects, err := r.Projects(ctx)
	if err != nil {
		return Project{}, false, err
	}
	for _, value := range projects {
		if value.ID == projectID {
			return value, true, nil
		}
	}

	return Project{}, false, nil
}

// Rows reads the state of every row of a project.
func (r *Repository) Rows(ctx context.Context, projectID string) (map[RowKey]RowState, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT table_name, row_key, revision, hub_tag, local_tag FROM sync_rows WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[RowKey]RowState{}
	for rows.Next() {
		var key RowKey
		var state RowState
		if err := rows.Scan(&key.Table, &key.Key, &state.Revision, &state.HubTag, &state.LocalTag); err != nil {
			return nil, err
		}
		result[key] = state
	}

	return result, rows.Err()
}

// Record stores row states and, when cursor is not nil, moves the cursor, in
// one transaction.
func (r *Repository) Record(ctx context.Context, projectID string, updates []Update, cursor *int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, update := range updates {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO sync_rows (project_id, table_name, row_key, revision, hub_tag, local_tag) VALUES (?, ?, ?, ?, ?, ?) "+
				"ON CONFLICT(project_id, table_name, row_key) DO UPDATE SET revision = excluded.revision, "+
				"hub_tag = excluded.hub_tag, local_tag = excluded.local_tag",
			projectID, update.Table, update.Key, update.Revision, update.HubTag, update.LocalTag)
		if err != nil {
			return err
		}
	}

	if cursor != nil {
		result, err := tx.ExecContext(ctx, "UPDATE sync_projects SET cursor = ?, synced_at = ? WHERE project_id = ?", *cursor, now(), projectID)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return errors.New("the project is not enabled for sync")
		}
	}

	return tx.Commit()
}

// Touch marks a finished sync.
func (r *Repository) Touch(ctx context.Context, projectID string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE sync_projects SET synced_at = ? WHERE project_id = ?", now(), projectID)

	return err
}

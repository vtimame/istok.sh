package server

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vtimame/istok.sh/internal/hub/protocol"
)

// Projects lists synced projects.
func (s *Store) Projects(ctx context.Context) ([]protocol.Project, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT id, meta FROM projects ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []protocol.Project{}
	for rows.Next() {
		var value protocol.Project
		if err := rows.Scan(&value.ID, &value.Meta); err != nil {
			return nil, err
		}
		result = append(result, value)
	}

	return result, rows.Err()
}

// PutProject creates a project or replaces its metadata.
func (s *Store) PutProject(ctx context.Context, value protocol.Project) error {
	now := s.timestamp()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO projects (id, meta, created_at, updated_at) VALUES (?, ?, ?, ?) "+
			"ON CONFLICT(id) DO UPDATE SET meta = excluded.meta, updated_at = excluded.updated_at",
		value.ID, value.Meta, now, now)

	return err
}

type storedRow struct {
	revision int64
	tag      string
	kind     string
}

// Push merges rows in one transaction and answers each row in order.
func (s *Store) Push(ctx context.Context, deviceID string, request protocol.PushRequest) ([]protocol.PushResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var known int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id = ?", request.ProjectID).Scan(&known); err != nil {
		return nil, err
	}
	if known == 0 {
		return nil, &protocol.Error{Code: protocol.CodeNotFound, Message: "the project is not registered on this hub"}
	}

	var seq int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq), 0) FROM rows").Scan(&seq); err != nil {
		return nil, err
	}

	now := s.timestamp()
	results := make([]protocol.PushResult, len(request.Rows))
	for index, row := range request.Rows {
		var current storedRow
		err := tx.QueryRowContext(ctx, "SELECT revision, tag, kind FROM rows WHERE project_id = ? AND table_name = ? AND row_key = ?",
			request.ProjectID, row.Table, row.Key).Scan(&current.revision, &current.tag, &current.kind)

		exists := true
		if errors.Is(err, sql.ErrNoRows) {
			exists = false
		} else if err != nil {
			return nil, err
		}

		status := decide(row, current, exists)
		results[index] = protocol.PushResult{Status: status}
		if status != protocol.StatusAccepted {
			results[index].Revision, results[index].Tag = current.revision, current.tag
			continue
		}

		seq++
		_, err = tx.ExecContext(ctx,
			"INSERT INTO rows (project_id, table_name, row_key, kind, revision, tag, schema_version, body, seq, device_id, updated_at) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) "+
				"ON CONFLICT(project_id, table_name, row_key) DO UPDATE SET kind = excluded.kind, revision = excluded.revision, "+
				"tag = excluded.tag, schema_version = excluded.schema_version, body = excluded.body, seq = excluded.seq, "+
				"device_id = excluded.device_id, updated_at = excluded.updated_at",
			request.ProjectID, row.Table, row.Key, row.Kind, row.Revision, row.Tag, row.SchemaVersion, row.Body, seq, deviceID, now)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return results, nil
}

// decide applies the merge rules of the row kind. They match the exchange
// import, so a hub and a direct import agree on the outcome.
func decide(row protocol.Row, current storedRow, exists bool) string {
	if !exists {
		return protocol.StatusAccepted
	}
	if current.tag == row.Tag {
		return protocol.StatusUnchanged
	}

	switch row.Kind {
	case protocol.KindMutable:
		switch {
		case row.Revision > current.revision:
			return protocol.StatusAccepted
		case row.Revision == current.revision:
			return protocol.StatusConflict
		default:
			return protocol.StatusStale
		}
	case protocol.KindGroup:
		if row.Revision < current.revision {
			return protocol.StatusStale
		}
		return protocol.StatusAccepted
	default:
		return protocol.StatusConflict
	}
}

// pullPageBytes bounds the bodies of one pull page; a page always carries at
// least one row, and rows are at most protocol.MaxRowBytes.
const pullPageBytes = 16 << 20

// Pull returns rows changed after the cursor. Rows the calling device wrote
// are included: a device that lost its database restores from the hub, and
// devices skip rows they already hold.
func (s *Store) Pull(ctx context.Context, projectID string, after int64, limit int) (protocol.PullResponse, error) {
	rows, err := s.read.QueryContext(ctx,
		"SELECT table_name, row_key, kind, revision, tag, schema_version, body, seq FROM rows "+
			"WHERE project_id = ? AND seq > ? ORDER BY seq LIMIT ?", projectID, after, limit+1)
	if err != nil {
		return protocol.PullResponse{}, err
	}
	defer rows.Close()

	response := protocol.PullResponse{Rows: []protocol.Row{}, Next: after}
	size := 0
	for rows.Next() {
		if len(response.Rows) == limit || (len(response.Rows) > 0 && size >= pullPageBytes) {
			response.More = true
			break
		}

		var value protocol.Row
		if err := rows.Scan(&value.Table, &value.Key, &value.Kind, &value.Revision, &value.Tag, &value.SchemaVersion,
			&value.Body, &value.Seq); err != nil {
			return protocol.PullResponse{}, err
		}

		size += len(value.Body)
		response.Rows = append(response.Rows, value)
		response.Next = value.Seq
	}

	return response, rows.Err()
}

package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/protocol"
)

// requireProject fails with not_found for a project the hub does not hold.
func requireProject(ctx context.Context, tx *sql.Tx, projectID string) error {
	var known int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id = ?", projectID).Scan(&known); err != nil {
		return err
	}
	if known == 0 {
		return &protocol.Error{Code: protocol.CodeNotFound, Message: "the project is not registered on this hub"}
	}

	return nil
}

// Numbers answers task number claims in order. A task keeps the number it
// already holds; otherwise it gets the requested number when free, or the
// next one after the highest number of the project.
func (s *Store) Numbers(ctx context.Context, request protocol.NumbersRequest) ([]protocol.NumberClaim, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireProject(ctx, tx, request.ProjectID); err != nil {
		return nil, err
	}

	var highest int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(number), 0) FROM task_numbers WHERE project_id = ?",
		request.ProjectID).Scan(&highest); err != nil {
		return nil, err
	}

	now := s.timestamp()
	result := make([]protocol.NumberClaim, len(request.Claims))
	for index, claim := range request.Claims {
		number, err := s.numberOf(ctx, tx, request.ProjectID, claim, highest)
		if err != nil {
			return nil, err
		}

		if number == 0 {
			highest++
			number = highest
			if _, err := tx.ExecContext(ctx, "INSERT INTO task_numbers (project_id, number, task_id, created_at) VALUES (?, ?, ?, ?)",
				request.ProjectID, number, claim.TaskID, now); err != nil {
				return nil, err
			}
		}
		highest = max(highest, number)

		result[index] = protocol.NumberClaim{TaskID: claim.TaskID, Number: number}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

// numberOf returns the number the task holds or takes the requested one when
// it is free; 0 asks the caller for the next number.
func (s *Store) numberOf(ctx context.Context, tx *sql.Tx, projectID string, claim protocol.NumberClaim, highest int64) (int64, error) {
	var held int64
	err := tx.QueryRowContext(ctx, "SELECT number FROM task_numbers WHERE project_id = ? AND task_id = ?",
		projectID, claim.TaskID).Scan(&held)
	if err == nil {
		return held, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	if claim.Number <= 0 || claim.Number > highest+protocol.MaxNumberClaims {
		return 0, nil
	}

	result, err := tx.ExecContext(ctx, "INSERT INTO task_numbers (project_id, number, task_id, created_at) VALUES (?, ?, ?, ?) "+
		"ON CONFLICT(project_id, number) DO NOTHING", projectID, claim.Number, claim.TaskID, s.timestamp())
	if err != nil {
		return 0, err
	}
	if inserted, err := result.RowsAffected(); err != nil || inserted == 0 {
		return 0, err
	}

	return claim.Number, nil
}

// Leases releases and acquires task leases for the calling device. A lease
// is granted when the task has none, the calling device holds it, or it
// expired; otherwise the result names the holder.
func (s *Store) Leases(ctx context.Context, device Device, request protocol.LeasesRequest) ([]protocol.LeaseResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireProject(ctx, tx, request.ProjectID); err != nil {
		return nil, err
	}

	for _, taskID := range request.Release {
		if _, err := tx.ExecContext(ctx, "DELETE FROM leases WHERE project_id = ? AND task_id = ? AND device_id = ?",
			request.ProjectID, taskID, device.ID); err != nil {
			return nil, err
		}
	}

	now := s.now()
	results := make([]protocol.LeaseResult, len(request.Acquire))
	for index, claim := range request.Acquire {
		result, err := s.acquire(ctx, tx, device, request.ProjectID, claim, now)
		if err != nil {
			return nil, err
		}
		results[index] = result
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return results, nil
}

func (s *Store) acquire(ctx context.Context, tx *sql.Tx, device Device, projectID string, claim protocol.LeaseClaim, now time.Time) (protocol.LeaseResult, error) {
	var holder protocol.LeaseHolder
	err := tx.QueryRowContext(ctx,
		"SELECT l.device_id, d.name, l.run_id, l.expires_at FROM leases l JOIN devices d ON d.id = l.device_id "+
			"WHERE l.project_id = ? AND l.task_id = ?", projectID, claim.TaskID).
		Scan(&holder.DeviceID, &holder.DeviceName, &holder.RunID, &holder.ExpiresAt)

	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return protocol.LeaseResult{}, err
	case holder.DeviceID != device.ID && holder.ExpiresAt > now.Format(timeFormat):
		return protocol.LeaseResult{TaskID: claim.TaskID, Holder: &holder}, nil
	}

	expires := now.Add(time.Duration(claim.TTLSeconds) * time.Second).Format(timeFormat)
	_, err = tx.ExecContext(ctx,
		"INSERT INTO leases (project_id, task_id, run_id, device_id, expires_at) VALUES (?, ?, ?, ?, ?) "+
			"ON CONFLICT(project_id, task_id) DO UPDATE SET run_id = excluded.run_id, device_id = excluded.device_id, "+
			"expires_at = excluded.expires_at", projectID, claim.TaskID, claim.RunID, device.ID, expires)
	if err != nil {
		return protocol.LeaseResult{}, err
	}

	return protocol.LeaseResult{TaskID: claim.TaskID, Granted: true}, nil
}

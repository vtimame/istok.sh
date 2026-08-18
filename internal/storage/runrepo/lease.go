package runrepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	runmodel "github.com/vtimame/istok.sh/internal/run"
)

func (r *Repository) Heartbeat(ctx context.Context, input runmodel.HeartbeatInput, actor runmodel.ActorSnapshot) (runmodel.Run, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Run{}, err
	}

	err := r.write(ctx, "heartbeat run", func(conn *sql.Conn) error {
		now := r.now().UTC()
		current, err := scanRun(conn.QueryRowContext(ctx, runQuery+`WHERE r.id=?`, input.RunID))
		if err != nil {
			return err
		}
		if current.Status != runmodel.StatusActive || current.LeaseID != input.LeaseID || current.LeaseOwner.ID != actor.ID {
			return runmodel.NewError(runmodel.CodeConflict, "run lease is not active or owned by actor")
		}
		if !current.ExpiresAt.After(now) {
			return runmodel.NewError(runmodel.CodeConflict, "run lease has expired and must be recovered")
		}

		result, err := conn.ExecContext(ctx, `
			UPDATE run_leases
			SET heartbeat_at=?, expires_at=?, updated_at=?
			WHERE run_id=? AND lease_id=? AND owner_id=?
			  AND EXISTS (SELECT 1 FROM runs WHERE id=run_leases.run_id AND status='active')
		`, stamp(now), stamp(now.Add(input.LeaseDuration)), stamp(now), input.RunID, input.LeaseID, actor.ID)
		if err != nil {
			return fmt.Errorf("heartbeat lease: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read heartbeat count: %w", err)
		}
		if count != 1 {
			return runmodel.NewError(runmodel.CodeConflict, "run lease is not active or owned by actor")
		}
		return nil
	})
	if err != nil {
		return runmodel.Run{}, err
	}
	return r.GetRun(ctx, input.RunID)
}

func (r *Repository) Recover(ctx context.Context, input runmodel.RecoverInput, actor runmodel.ActorSnapshot) (runmodel.Run, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	if !runmodel.IsUUIDv7(input.EventID) {
		return runmodel.Run{}, runmodel.NewError(runmodel.CodeInvalid, "recovery event id must be a canonical UUIDv7")
	}

	err := r.write(ctx, "recover run", func(conn *sql.Conn) error {
		current, err := scanRun(conn.QueryRowContext(ctx, runQuery+`WHERE r.id=?`, input.RunID))
		if err != nil {
			return err
		}
		if current.Status != runmodel.StatusActive {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "recovery requires an active run")
		}
		now := r.now().UTC()
		if current.LeaseOwner.ID != actor.ID {
			return runmodel.NewError(runmodel.CodeConflict, "run recovery requires the current lease owner")
		}
		if !input.Force && current.ExpiresAt.After(now) {
			return runmodel.NewError(runmodel.CodeConflict, "run lease has not expired")
		}
		if _, err := conn.ExecContext(ctx, `UPDATE executions SET revision=revision+1,status='cancelled',signal='recovered',updated_at=?,finished_at=? WHERE run_id=? AND status='running'`, stamp(now), stamp(now), input.RunID); err != nil {
			return fmt.Errorf("cancel recovered executions: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `UPDATE run_leases SET lease_id=?,owner_id=?,owner_kind=?,owner_name=?,heartbeat_at=?,expires_at=?,updated_at=? WHERE run_id=?`, input.LeaseID, actor.ID, actor.Kind, actor.Name, stamp(now), stamp(now.Add(input.LeaseDuration)), stamp(now), input.RunID); err != nil {
			return fmt.Errorf("rotate run lease: %w", err)
		}
		result, err := conn.ExecContext(ctx, `UPDATE runs SET revision=revision+1,updated_at=? WHERE id=? AND status='active'`, stamp(now), input.RunID)
		if err != nil {
			return fmt.Errorf("update recovered run: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read recovered run count: %w", err)
		}
		if count != 1 {
			return runmodel.NewError(runmodel.CodeConflict, "run recovery conflict")
		}
		var taskRevision int64
		if err := conn.QueryRowContext(ctx, `SELECT revision FROM tasks WHERE id=?`, current.TaskID).Scan(&taskRevision); err != nil {
			return fmt.Errorf("read recovery task revision: %w", err)
		}
		_, err = conn.ExecContext(ctx, `INSERT INTO task_events (id,task_id,type,body,task_revision,actor_id,actor_kind,actor_name,created_at) VALUES (?,?,'run_recovered',?,?,?,?,?,?)`, input.EventID, current.TaskID, input.Reason, taskRevision, actor.ID, actor.Kind, actor.Name, stamp(now))
		return mapSQLError(err, "recovery event conflicts with existing data")
	})
	if err != nil {
		return runmodel.Run{}, err
	}
	return r.GetRun(ctx, input.RunID)
}

func (r *Repository) Abandon(ctx context.Context, input runmodel.AbandonInput, actor runmodel.ActorSnapshot) (runmodel.Run, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	err := r.write(ctx, "abandon run", func(conn *sql.Conn) error {
		return abandonRun(ctx, conn, input.RunID, input.ExpectedRevision, actor, input.EventID, input.Reason, r.now().UTC(), true)
	})
	if err != nil {
		return runmodel.Run{}, err
	}
	return r.GetRun(ctx, input.RunID)
}

func abandonRun(ctx context.Context, conn *sql.Conn, runID string, expectedRevision int64, actor runmodel.ActorSnapshot, eventID, reason string, now time.Time, checkRevision bool) error {
	current, err := scanRun(conn.QueryRowContext(ctx, runQuery+`WHERE r.id=?`, runID))
	if err != nil {
		return err
	}
	if checkRevision && current.Revision != expectedRevision {
		return runmodel.NewError(runmodel.CodeRevisionConflict, "run revision conflict")
	}
	if current.Status != runmodel.StatusActive {
		return runmodel.NewError(runmodel.CodeInvalidTransition, "abandonment requires an active run")
	}
	if _, err := conn.ExecContext(ctx, `UPDATE executions SET revision=revision+1,status='cancelled',signal='abandoned',updated_at=?,finished_at=? WHERE run_id=? AND status='running'`, stamp(now), stamp(now), runID); err != nil {
		return fmt.Errorf("cancel abandoned executions: %w", err)
	}
	query := `UPDATE runs SET revision=revision+1,status='abandoned',updated_at=?,finished_at=?,finished_actor_id=?,finished_actor_kind=?,finished_actor_name=?,result_summary=? WHERE id=? AND status='active'`
	args := []any{stamp(now), stamp(now), actor.ID, actor.Kind, actor.Name, reason, runID}
	if checkRevision {
		query += ` AND revision=?`
		args = append(args, expectedRevision)
	}
	result, err := conn.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("abandon run: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read abandoned run count: %w", err)
	}
	if count != 1 {
		return runmodel.NewError(runmodel.CodeRevisionConflict, "run revision conflict")
	}
	var taskRevision int64
	if err := conn.QueryRowContext(ctx, `SELECT revision FROM tasks WHERE id=?`, current.TaskID).Scan(&taskRevision); err != nil {
		return fmt.Errorf("read abandonment task revision: %w", err)
	}
	body := fmt.Sprintf("run_id=%s\n%s", runID, reason)
	_, err = conn.ExecContext(ctx, `INSERT INTO task_events (id,task_id,type,body,task_revision,actor_id,actor_kind,actor_name,created_at) VALUES (?,?,'run_abandoned',?,?,?,?,?,?)`, eventID, current.TaskID, body, taskRevision, actor.ID, actor.Kind, actor.Name, stamp(now))
	return mapSQLError(err, "abandonment event conflicts with existing data")
}

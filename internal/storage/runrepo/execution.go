package runrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	runmodel "s26.dev/istok-cli/internal/run"
)

func (r *Repository) StartExecution(ctx context.Context, input runmodel.StartExecutionInput, actor runmodel.ActorSnapshot) (runmodel.Execution, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Execution{}, err
	}
	if !runmodel.IsUUIDv7(input.ID) {
		return runmodel.Execution{}, runmodel.NewError(runmodel.CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Execution{}, err
	}

	argv, err := json.Marshal(input.Argv)
	if err != nil {
		return runmodel.Execution{}, fmt.Errorf("encode execution argv: %w", err)
	}

	err = r.write(ctx, "start execution", func(conn *sql.Conn) error {
		current, err := scanRun(conn.QueryRowContext(ctx, runQuery+`WHERE r.id=?`, input.RunID))
		if err != nil {
			return err
		}
		if current.Status != runmodel.StatusActive {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "execution requires an active run")
		}
		now := r.now().UTC()
		if current.LeaseID != input.LeaseID || current.LeaseOwner.ID != actor.ID {
			return runmodel.NewError(runmodel.CodeConflict, "execution requires the current owned run lease")
		}
		if !current.ExpiresAt.After(now) {
			return runmodel.NewError(runmodel.CodeConflict, "run lease has expired and must be recovered")
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO executions (
				id,run_id,revision,status,argv,cwd,
				dangerous_override,actor_id,actor_kind,actor_name,started_at,updated_at
			) VALUES (?,?,1,'running',?,?,?,?,?,?,?,?)
		`,
			input.ID,
			input.RunID,
			string(argv),
			input.CWD,
			input.DangerousOverride,
			actor.ID,
			actor.Kind,
			actor.Name,
			stamp(now),
			stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "execution conflicts with existing data")
		}

		return nil
	})
	if err != nil {
		return runmodel.Execution{}, err
	}

	return r.GetExecution(ctx, input.ID)
}

func (r *Repository) GetExecution(ctx context.Context, id string) (runmodel.Execution, error) {
	if !runmodel.IsUUIDv7(id) {
		return runmodel.Execution{}, runmodel.NewError(runmodel.CodeInvalid, "execution id must be a canonical UUIDv7")
	}

	return scanExecution(r.db.QueryRowContext(ctx, executionQuery+`WHERE id=?`, id))
}

func (r *Repository) FinishExecution(ctx context.Context, input runmodel.FinishExecutionInput, actor runmodel.ActorSnapshot) (runmodel.Execution, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Execution{}, err
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Execution{}, err
	}

	err := r.write(ctx, "finish execution", func(conn *sql.Conn) error {
		current, err := scanExecution(conn.QueryRowContext(ctx, executionQuery+`WHERE id=?`, input.ExecutionID))
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "execution revision conflict")
		}
		if current.Status != runmodel.ExecutionRunning {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "execution is already terminal")
		}

		now := r.now().UTC()
		result, err := conn.ExecContext(ctx, `
			UPDATE executions
			SET revision=revision+1,
			    status=?,
			    exit_code=?,
			    duration_ms=?,
			    signal=?,
			    timed_out=?,
			    updated_at=?,
			    finished_at=?
			WHERE id=? AND revision=? AND status='running'
		`,
			input.Status,
			input.ExitCode,
			input.DurationMS,
			input.Signal,
			input.TimedOut,
			stamp(now),
			stamp(now),
			input.ExecutionID,
			input.ExpectedRevision,
		)
		if err != nil {
			return fmt.Errorf("finish execution: %w", err)
		}

		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read finished execution count: %w", err)
		}
		if updated != 1 {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "execution revision conflict")
		}

		return nil
	})
	if err != nil {
		return runmodel.Execution{}, err
	}

	return r.GetExecution(ctx, input.ExecutionID)
}

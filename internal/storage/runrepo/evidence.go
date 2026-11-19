package runrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	runmodel "github.com/vtimame/istok.sh/internal/run"
)

// RecordEvidence stores a check made outside Istok: a terminal record-only
// execution and an attested validation, in one transaction.
func (r *Repository) RecordEvidence(ctx context.Context, input runmodel.RecordEvidenceInput, actor runmodel.ActorSnapshot) (runmodel.Evidence, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Evidence{}, err
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Evidence{}, err
	}

	command := input.EvidenceCommand()
	argv, err := json.Marshal([]string{command})
	if err != nil {
		return runmodel.Evidence{}, fmt.Errorf("encode evidence argv: %w", err)
	}

	executionStatus := runmodel.ExecutionSucceeded
	if input.Status == runmodel.ValidationStatusFailed {
		executionStatus = runmodel.ExecutionFailed
	}

	err = r.write(ctx, "record evidence", func(conn *sql.Conn) error {
		current, err := scanRun(conn.QueryRowContext(ctx, runQuery+`WHERE r.id=?`, input.RunID))
		if err != nil {
			return err
		}
		if current.Status != runmodel.StatusActive {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "evidence requires an active run")
		}

		now := r.now().UTC()
		if current.LeaseID != input.LeaseID || current.LeaseOwner.ID != actor.ID {
			return runmodel.NewError(runmodel.CodeConflict, "evidence requires the current owned run lease")
		}
		if !current.ExpiresAt.After(now) {
			return runmodel.NewError(runmodel.CodeConflict, "run lease has expired and must be recovered")
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO executions (
				id,run_id,revision,status,argv,cwd,exit_code,duration_ms,
				actor_id,actor_kind,actor_name,started_at,updated_at,finished_at
			) VALUES (?,?,1,?,?,?,?,?,?,?,?,?,?,?)
		`,
			input.ExecutionID, input.RunID, executionStatus, string(argv), input.CWD, input.ExitCode, input.DurationMS,
			actor.ID, actor.Kind, actor.Name, stamp(now), stamp(now), stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "evidence execution conflicts with existing data")
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO validations (
				id,execution_id,source,status,command,exit_code,duration_ms,summary,
				actor_id,actor_kind,actor_name,created_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		`,
			input.ValidationID, input.ExecutionID, runmodel.ValidationSourceAttested, input.Status, command,
			input.ExitCode, input.DurationMS, input.EvidenceSummary(),
			actor.ID, actor.Kind, actor.Name, stamp(now),
		)

		return mapSQLError(err, "evidence validation conflicts with existing data")
	})
	if err != nil {
		return runmodel.Evidence{}, err
	}

	execution, err := r.GetExecution(ctx, input.ExecutionID)
	if err != nil {
		return runmodel.Evidence{}, err
	}
	validation, err := r.GetValidation(ctx, input.ValidationID)
	if err != nil {
		return runmodel.Evidence{}, err
	}

	return runmodel.Evidence{Execution: execution, Validation: validation}, nil
}

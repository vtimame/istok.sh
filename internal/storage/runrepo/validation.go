package runrepo

import (
	"context"
	"database/sql"
	"fmt"

	runmodel "s26.dev/istok-cli/internal/run"
)

func (r *Repository) RecordValidation(ctx context.Context, input runmodel.RecordValidationInput, actor runmodel.ActorSnapshot) (runmodel.Validation, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Validation{}, err
	}
	if !runmodel.IsUUIDv7(input.ID) {
		return runmodel.Validation{}, runmodel.NewError(runmodel.CodeInvalid, "validation id must be a canonical UUIDv7")
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Validation{}, err
	}
	if input.Source != runmodel.ValidationSourceAttested {
		return runmodel.Validation{}, runmodel.NewError(runmodel.CodeInvalid, "manual validations must be attested")
	}

	err := r.write(ctx, "record validation", func(conn *sql.Conn) error {
		var executionStatus runmodel.ExecutionStatus
		var runStatus runmodel.Status
		err := conn.QueryRowContext(ctx, `
			SELECT e.status,r.status
			FROM executions e
			JOIN runs r ON r.id=e.run_id
			WHERE e.id=?
		`, input.ExecutionID).Scan(&executionStatus, &runStatus)
		if err == sql.ErrNoRows {
			return runmodel.NewError(runmodel.CodeNotFound, "execution was not found")
		}
		if err != nil {
			return fmt.Errorf("load execution for validation: %w", err)
		}
		if !executionStatus.Terminal() {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "validation requires a terminal execution")
		}
		if runStatus != runmodel.StatusActive {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "validation requires an active run")
		}

		now := r.now().UTC()
		_, err = conn.ExecContext(ctx, `
			INSERT INTO validations (
				id,execution_id,source,status,command,exit_code,duration_ms,summary,
				actor_id,actor_kind,actor_name,created_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		`,
			input.ID,
			input.ExecutionID,
			input.Source,
			input.Status,
			input.Command,
			input.ExitCode,
			input.DurationMS,
			input.Summary,
			actor.ID,
			actor.Kind,
			actor.Name,
			stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "validation conflicts with existing data")
		}

		return nil
	})
	if err != nil {
		return runmodel.Validation{}, err
	}

	return r.GetValidation(ctx, input.ID)
}

func (r *Repository) GetValidation(ctx context.Context, id string) (runmodel.Validation, error) {
	if !runmodel.IsUUIDv7(id) {
		return runmodel.Validation{}, runmodel.NewError(runmodel.CodeInvalid, "validation id must be a canonical UUIDv7")
	}

	return scanValidation(r.db.QueryRowContext(ctx, validationQuery+`WHERE v.id=?`, id))
}

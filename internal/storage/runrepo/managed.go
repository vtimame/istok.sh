package runrepo

import (
	"context"
	"database/sql"
	"fmt"

	runmodel "github.com/vtimame/istok.sh/internal/run"
)

func (r *Repository) FinishManagedExecution(ctx context.Context, input runmodel.FinishManagedExecutionInput, actor runmodel.ActorSnapshot) (runmodel.FinishManagedExecutionResult, error) {
	if err := input.Validate(); err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	if err := actor.Validate(); err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	if input.Validation != nil && input.Validation.Source != runmodel.ValidationSourceExecuted {
		return runmodel.FinishManagedExecutionResult{}, runmodel.NewError(runmodel.CodeInvalid, "managed validation source must be executed")
	}
	for _, artifact := range input.Artifacts {
		if !runmodel.IsUUIDv7(artifact.ID) {
			return runmodel.FinishManagedExecutionResult{}, runmodel.NewError(runmodel.CodeInvalid, "artifact id must be a canonical UUIDv7")
		}
	}
	if input.Validation != nil && !runmodel.IsUUIDv7(input.Validation.ID) {
		return runmodel.FinishManagedExecutionResult{}, runmodel.NewError(runmodel.CodeInvalid, "validation id must be a canonical UUIDv7")
	}

	err := r.write(ctx, "finish managed execution", func(conn *sql.Conn) error {
		current, err := scanExecution(conn.QueryRowContext(ctx, executionQuery+`WHERE id=?`, input.ExecutionID))
		if err != nil {
			return err
		}
		if current.Status != runmodel.ExecutionRunning || current.Revision != input.ExpectedRevision {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "execution is not the expected running revision")
		}
		now := r.now().UTC()
		result, err := conn.ExecContext(ctx, `UPDATE executions SET revision=revision+1,status=?,exit_code=?,duration_ms=?,signal=?,timed_out=?,updated_at=?,finished_at=? WHERE id=? AND revision=? AND status='running'`, input.Status, input.ExitCode, input.DurationMS, input.Signal, input.TimedOut, stamp(now), stamp(now), input.ExecutionID, input.ExpectedRevision)
		if err != nil {
			return fmt.Errorf("finish managed execution: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read managed execution count: %w", err)
		}
		if count != 1 {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "execution revision conflict")
		}
		for _, artifact := range input.Artifacts {
			_, err = conn.ExecContext(ctx, `INSERT INTO artifacts (id,run_id,execution_id,kind,relative_path,sha256,original_size,stored_size,truncated,media_type,actor_id,actor_kind,actor_name,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, artifact.ID, current.RunID, input.ExecutionID, artifact.Kind, artifact.RelativePath, artifact.SHA256, artifact.OriginalSize, artifact.StoredSize, artifact.Truncated, artifact.MediaType, actor.ID, actor.Kind, actor.Name, stamp(now))
			if err != nil {
				return mapSQLError(err, "managed artifact conflicts with existing data")
			}
		}
		if validation := input.Validation; validation != nil {
			if validation.ExitCode == nil || input.ExitCode == nil || *validation.ExitCode != *input.ExitCode || validation.DurationMS == nil || input.DurationMS == nil || *validation.DurationMS != *input.DurationMS {
				return runmodel.NewError(runmodel.CodeInvalid, "executed validation must match execution result")
			}
			expected := runmodel.ValidationStatusFailed
			if input.Status == runmodel.ExecutionSucceeded && *input.ExitCode == 0 {
				expected = runmodel.ValidationStatusPassed
			}
			if validation.Status != expected {
				return runmodel.NewError(runmodel.CodeInvalid, "executed validation status does not match execution result")
			}
			_, err = conn.ExecContext(ctx, `INSERT INTO validations (id,execution_id,source,status,command,exit_code,duration_ms,summary,actor_id,actor_kind,actor_name,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, validation.ID, input.ExecutionID, validation.Source, validation.Status, validation.Command, validation.ExitCode, validation.DurationMS, validation.Summary, actor.ID, actor.Kind, actor.Name, stamp(now))
			if err != nil {
				return mapSQLError(err, "managed validation conflicts with existing data")
			}
		}
		return nil
	})
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	execution, err := r.GetExecution(ctx, input.ExecutionID)
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	artifacts, err := r.ListArtifacts(ctx, input.ExecutionID)
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	result := runmodel.FinishManagedExecutionResult{Execution: execution, Artifacts: artifacts}
	if input.Validation != nil {
		validation, err := r.GetValidation(ctx, input.Validation.ID)
		if err != nil {
			return runmodel.FinishManagedExecutionResult{}, err
		}
		result.Validation = &validation
	}
	return result, nil
}

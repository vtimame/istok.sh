package runrepo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	runmodel "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

func (r *Repository) FinishRun(ctx context.Context, input runmodel.FinishRunInput, actor runmodel.ActorSnapshot) (runmodel.Run, error) {
	if err := input.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	if !runmodel.IsUUIDv7(input.EventID) {
		return runmodel.Run{}, runmodel.NewError(runmodel.CodeInvalid, "event id must be a canonical UUIDv7")
	}
	if err := actor.Validate(); err != nil {
		return runmodel.Run{}, err
	}

	err := r.write(ctx, "finish run", func(conn *sql.Conn) error {
		current, err := scanRun(conn.QueryRowContext(ctx, runQuery+`WHERE r.id=?`, input.RunID))
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "run revision conflict")
		}
		if current.Status != runmodel.StatusActive {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "run is already terminal")
		}
		now := r.now().UTC()
		if current.LeaseID != input.LeaseID || current.LeaseOwner.ID != actor.ID {
			return runmodel.NewError(runmodel.CodeConflict, "run finish requires the current owned lease")
		}
		if !current.ExpiresAt.After(now) {
			return runmodel.NewError(runmodel.CodeConflict, "run lease has expired and must be recovered")
		}

		var activeExecutions int
		err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM executions WHERE run_id=? AND status='running'`, input.RunID).Scan(&activeExecutions)
		if err != nil {
			return fmt.Errorf("check active executions: %w", err)
		}
		if activeExecutions > 0 {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "run has active executions")
		}

		overrideReason := ""
		if input.Status == runmodel.StatusSucceeded {
			if input.AllowUnvalidated {
				overrideReason = strings.TrimSpace(input.OverrideReason)
			} else {
				passed, err := hasPassedValidation(ctx, conn, input.RunID)
				if err != nil {
					return err
				}
				if !passed {
					return runmodel.NewError(runmodel.CodeEvidenceRequired, "a passed validation is required to finish the run successfully")
				}
			}
		}

		result, err := conn.ExecContext(ctx, `
			UPDATE runs
			SET revision=revision+1,
			    status=?,
			    updated_at=?,
			    finished_at=?,
			    finished_actor_id=?,
			    finished_actor_kind=?,
			    finished_actor_name=?,
			    result_summary=?,
			    validation_override=?
			WHERE id=? AND revision=? AND status='active'
		`,
			input.Status,
			stamp(now),
			stamp(now),
			actor.ID,
			actor.Kind,
			actor.Name,
			strings.TrimSpace(input.ResultSummary),
			overrideReason,
			input.RunID,
			input.ExpectedRevision,
		)
		if err != nil {
			return fmt.Errorf("finish run: %w", err)
		}

		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read finished run count: %w", err)
		}
		if updated != 1 {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "run revision conflict")
		}

		var taskRevision int64
		if err := conn.QueryRowContext(ctx, `SELECT revision FROM tasks WHERE id=?`, current.TaskID).Scan(&taskRevision); err != nil {
			return fmt.Errorf("read task revision for run finish: %w", err)
		}
		_, err = conn.ExecContext(ctx, `
			INSERT INTO task_events (
				id,task_id,type,body,task_revision,
				actor_id,actor_kind,actor_name,created_at
			) VALUES (?,?,'run_finished',?,?,?,?,?,?)
		`,
			input.EventID,
			current.TaskID,
			fmt.Sprintf("%s\n%s", input.RunID, strings.TrimSpace(input.ResultSummary)),
			taskRevision,
			actor.ID,
			actor.Kind,
			actor.Name,
			stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "run finish event conflicts with existing data")
		}

		return nil
	})
	if err != nil {
		return runmodel.Run{}, err
	}

	return r.GetRun(ctx, input.RunID)
}

func hasPassedValidation(ctx context.Context, conn *sql.Conn, runID string) (bool, error) {
	var exists int
	err := conn.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM validations v
			JOIN executions e ON e.id=v.execution_id
			WHERE e.run_id=? AND v.status='passed'
		)
	`, runID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check passed validation: %w", err)
	}

	return exists != 0, nil
}

func (r *Repository) CompleteTask(ctx context.Context, input runmodel.CompletionRecord) (task.Task, runmodel.Completion, error) {
	if !runmodel.IsUUIDv7(input.ID) || !runmodel.IsUUIDv7(input.TaskID) || !runmodel.IsUUIDv7(input.EventID) {
		return task.Task{}, runmodel.Completion{}, runmodel.NewError(runmodel.CodeInvalid, "completion, task, and event ids must be canonical UUIDv7")
	}
	if input.ExpectedTaskRevision < 1 {
		return task.Task{}, runmodel.Completion{}, runmodel.NewError(runmodel.CodeInvalid, "expected task revision is required")
	}
	if strings.TrimSpace(input.Note) == "" {
		return task.Task{}, runmodel.Completion{}, runmodel.NewError(runmodel.CodeInvalid, "completion note is required")
	}
	if err := input.Actor.Validate(); err != nil {
		return task.Task{}, runmodel.Completion{}, err
	}

	var completed task.Task
	var completion runmodel.Completion
	err := r.write(ctx, "complete task", func(conn *sql.Conn) error {
		current, err := scanTask(conn.QueryRowContext(ctx, taskQuery+`WHERE id=?`, input.TaskID))
		if err != nil {
			return err
		}
		if current.DeletedAt != nil || current.Status == task.StatusDone {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "task cannot be completed")
		}
		if current.Revision != input.ExpectedTaskRevision {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "task revision conflict")
		}

		var activeRuns int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE task_id=? AND status='active'`, input.TaskID).Scan(&activeRuns); err != nil {
			return fmt.Errorf("check active run before task completion: %w", err)
		}
		if activeRuns > 0 {
			return runmodel.NewError(runmodel.CodeConflict, "task has an active run")
		}

		overrideReason := strings.TrimSpace(input.OverrideReason)
		if err := validateCompletionEvidence(ctx, conn, input.TaskID, input.RunID, input.ValidationID, overrideReason); err != nil {
			return err
		}

		now := r.now().UTC()
		_, err = conn.ExecContext(ctx, `
			INSERT INTO task_completions (
				id,task_id,run_id,validation_id,note,override_reason,
				actor_id,actor_kind,actor_name,created_at
			) VALUES (?,?,?,?,?,?,?,?,?,?)
		`,
			input.ID,
			input.TaskID,
			nullableString(input.RunID),
			nullableString(input.ValidationID),
			strings.TrimSpace(input.Note),
			overrideReason,
			input.Actor.ID,
			input.Actor.Kind,
			input.Actor.Name,
			stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "task completion conflicts with existing data")
		}

		result, err := conn.ExecContext(ctx, `
			UPDATE tasks
			SET revision=revision+1,status='done',updated_at=?
			WHERE id=? AND revision=? AND deleted_at IS NULL AND status!='done'
		`, stamp(now), input.TaskID, input.ExpectedTaskRevision)
		if err != nil {
			return fmt.Errorf("complete task: %w", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read completed task count: %w", err)
		}
		if updated != 1 {
			return runmodel.NewError(runmodel.CodeRevisionConflict, "task revision conflict")
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO task_events (
				id,task_id,type,body,task_revision,
				actor_id,actor_kind,actor_name,created_at
			) VALUES (?,?,'completed',?,?,?,?,?,?)
		`,
			input.EventID,
			input.TaskID,
			fmt.Sprintf("%s\n%s", input.ID, strings.TrimSpace(input.Note)),
			input.ExpectedTaskRevision+1,
			input.Actor.ID,
			input.Actor.Kind,
			input.Actor.Name,
			stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "task completion event conflicts with existing data")
		}

		completed, err = scanTask(conn.QueryRowContext(ctx, taskQuery+`WHERE id=?`, input.TaskID))
		if err != nil {
			return err
		}
		completion = runmodel.Completion{
			ID:             input.ID,
			TaskID:         input.TaskID,
			RunID:          input.RunID,
			ValidationID:   input.ValidationID,
			Note:           strings.TrimSpace(input.Note),
			OverrideReason: overrideReason,
			Actor:          input.Actor,
			CreatedAt:      now,
		}

		return nil
	})
	if err != nil {
		return task.Task{}, runmodel.Completion{}, err
	}

	return completed, completion, nil
}

func validateCompletionEvidence(
	ctx context.Context,
	conn *sql.Conn,
	taskID string,
	runID *string,
	validationID *string,
	overrideReason string,
) error {
	if runID == nil && validationID == nil {
		if overrideReason == "" {
			return runmodel.NewError(runmodel.CodeEvidenceRequired, "passed validation evidence or an override is required")
		}

		return nil
	}

	if runID != nil && validationID != nil {
		var evidenceTaskID string
		var runStatus runmodel.Status
		var validationStatus runmodel.ValidationStatus
		err := conn.QueryRowContext(ctx, `
			SELECT r.task_id,r.status,v.status
			FROM runs r
			JOIN executions e ON e.run_id=r.id
			JOIN validations v ON v.execution_id=e.id
			WHERE r.id=? AND v.id=?
		`, *runID, *validationID).Scan(&evidenceTaskID, &runStatus, &validationStatus)
		if err == sql.ErrNoRows {
			if overrideReason == "" {
				return runmodel.NewError(runmodel.CodeEvidenceRequired, "completion requires a passed validation from the succeeded run")
			}

			return runmodel.NewError(runmodel.CodeConflict, "completion validation does not belong to the run")
		}
		if err != nil {
			return fmt.Errorf("validate completion evidence: %w", err)
		}
		if evidenceTaskID != taskID {
			return runmodel.NewError(runmodel.CodeConflict, "completion evidence belongs to another task")
		}
		if runStatus == runmodel.StatusSucceeded && validationStatus == runmodel.ValidationStatusPassed {
			return nil
		}
		if overrideReason == "" {
			return runmodel.NewError(runmodel.CodeEvidenceRequired, "completion requires a passed validation from the succeeded run")
		}

		return nil
	}

	if validationID != nil {
		var evidenceTaskID string
		err := conn.QueryRowContext(ctx, `
			SELECT r.task_id
			FROM validations v
			JOIN executions e ON e.id=v.execution_id
			JOIN runs r ON r.id=e.run_id
			WHERE v.id=?
		`, *validationID).Scan(&evidenceTaskID)
		if err == sql.ErrNoRows {
			return runmodel.NewError(runmodel.CodeNotFound, "completion validation was not found")
		}
		if err != nil {
			return fmt.Errorf("validate completion validation: %w", err)
		}
		if evidenceTaskID != taskID {
			return runmodel.NewError(runmodel.CodeConflict, "completion validation belongs to another task")
		}
	}

	if runID != nil {
		var evidenceTaskID string
		err := conn.QueryRowContext(ctx, `SELECT task_id FROM runs WHERE id=?`, *runID).Scan(&evidenceTaskID)
		if err == sql.ErrNoRows {
			return runmodel.NewError(runmodel.CodeNotFound, "completion run was not found")
		}
		if err != nil {
			return fmt.Errorf("validate completion run: %w", err)
		}
		if evidenceTaskID != taskID {
			return runmodel.NewError(runmodel.CodeConflict, "completion run belongs to another task")
		}
	}

	if overrideReason == "" {
		return runmodel.NewError(runmodel.CodeEvidenceRequired, "run id and validation id are required unless an override is provided")
	}

	return nil
}

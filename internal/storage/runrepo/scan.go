package runrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	runmodel "s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

type scanner interface {
	Scan(...any) error
}

const runQuery = `
SELECT r.id,r.task_id,r.context_snapshot_id,r.revision,r.status,
       r.actor_id,r.actor_kind,r.actor_name,r.base_branch,r.base_commit,
       r.started_at,r.updated_at,r.finished_at,
       r.finished_actor_id,r.finished_actor_kind,r.finished_actor_name,
       r.result_summary,r.validation_override,
       l.lease_id,l.owner_id,l.owner_kind,l.owner_name,l.heartbeat_at,l.expires_at
FROM runs r JOIN run_leases l ON l.run_id=r.id `

func scanRun(row scanner) (runmodel.Run, error) {
	var value runmodel.Run
	var startedAt, updatedAt string
	var finishedAt sql.NullString
	var heartbeatAt, expiresAt string
	var finishedActorID, finishedActorKind, finishedActorName sql.NullString

	err := row.Scan(
		&value.ID,
		&value.TaskID,
		&value.ContextSnapshotID,
		&value.Revision,
		&value.Status,
		&value.Actor.ID,
		&value.Actor.Kind,
		&value.Actor.Name,
		&value.BaseBranch,
		&value.BaseCommit,
		&startedAt,
		&updatedAt,
		&finishedAt,
		&finishedActorID,
		&finishedActorKind,
		&finishedActorName,
		&value.ResultSummary,
		&value.ValidationOverride,
		&value.LeaseID,
		&value.LeaseOwner.ID,
		&value.LeaseOwner.Kind,
		&value.LeaseOwner.Name,
		&heartbeatAt,
		&expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return runmodel.Run{}, runmodel.NewError(runmodel.CodeNotFound, "run was not found")
	}
	if err != nil {
		return runmodel.Run{}, fmt.Errorf("scan run: %w", err)
	}

	value.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return runmodel.Run{}, fmt.Errorf("parse run start time: %w", err)
	}
	value.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return runmodel.Run{}, fmt.Errorf("parse run update time: %w", err)
	}
	if finishedAt.Valid {
		parsed, err := parseTime(finishedAt.String)
		if err != nil {
			return runmodel.Run{}, fmt.Errorf("parse run finish time: %w", err)
		}
		value.FinishedAt = &parsed
	}
	if finishedActorID.Valid || finishedActorKind.Valid || finishedActorName.Valid {
		value.FinishedBy = &runmodel.ActorSnapshot{
			ID:   finishedActorID.String,
			Kind: finishedActorKind.String,
			Name: finishedActorName.String,
		}
	}
	value.HeartbeatAt, err = parseTime(heartbeatAt)
	if err != nil {
		return runmodel.Run{}, fmt.Errorf("parse lease heartbeat: %w", err)
	}
	value.ExpiresAt, err = parseTime(expiresAt)
	if err != nil {
		return runmodel.Run{}, fmt.Errorf("parse lease expiration: %w", err)
	}

	return value, nil
}

const executionQuery = `
SELECT id,run_id,revision,status,argv,cwd,exit_code,duration_ms,signal,timed_out,dangerous_override,
       actor_id,actor_kind,actor_name,started_at,updated_at,finished_at
FROM executions `

func scanExecution(row scanner) (runmodel.Execution, error) {
	var value runmodel.Execution
	var argv string
	var exitCode, durationMS sql.NullInt64
	var timedOut int
	var startedAt, updatedAt string
	var finishedAt sql.NullString

	err := row.Scan(
		&value.ID,
		&value.RunID,
		&value.Revision,
		&value.Status,
		&argv,
		&value.CWD,
		&exitCode,
		&durationMS,
		&value.Signal,
		&timedOut,
		&value.DangerousOverride,
		&value.Actor.ID,
		&value.Actor.Kind,
		&value.Actor.Name,
		&startedAt,
		&updatedAt,
		&finishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return runmodel.Execution{}, runmodel.NewError(runmodel.CodeNotFound, "execution was not found")
	}
	if err != nil {
		return runmodel.Execution{}, fmt.Errorf("scan execution: %w", err)
	}

	if err := json.Unmarshal([]byte(argv), &value.Argv); err != nil {
		return runmodel.Execution{}, fmt.Errorf("decode execution argv: %w", err)
	}
	if exitCode.Valid {
		exit := int(exitCode.Int64)
		value.ExitCode = &exit
	}
	if durationMS.Valid {
		duration := durationMS.Int64
		value.DurationMS = &duration
	}
	value.TimedOut = timedOut != 0

	value.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return runmodel.Execution{}, fmt.Errorf("parse execution start time: %w", err)
	}
	value.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return runmodel.Execution{}, fmt.Errorf("parse execution update time: %w", err)
	}
	if finishedAt.Valid {
		parsed, err := parseTime(finishedAt.String)
		if err != nil {
			return runmodel.Execution{}, fmt.Errorf("parse execution finish time: %w", err)
		}
		value.FinishedAt = &parsed
	}

	return value, nil
}

const validationQuery = `
SELECT v.id,v.execution_id,v.source,v.status,v.command,v.exit_code,v.duration_ms,v.summary,
       v.actor_id,v.actor_kind,v.actor_name,v.created_at
FROM validations v `

func scanValidation(row scanner) (runmodel.Validation, error) {
	var value runmodel.Validation
	var exitCode, durationMS sql.NullInt64
	var createdAt string

	err := row.Scan(
		&value.ID,
		&value.ExecutionID,
		&value.Source,
		&value.Status,
		&value.Command,
		&exitCode,
		&durationMS,
		&value.Summary,
		&value.Actor.ID,
		&value.Actor.Kind,
		&value.Actor.Name,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return runmodel.Validation{}, runmodel.NewError(runmodel.CodeNotFound, "validation was not found")
	}
	if err != nil {
		return runmodel.Validation{}, fmt.Errorf("scan validation: %w", err)
	}

	if exitCode.Valid {
		exit := int(exitCode.Int64)
		value.ExitCode = &exit
	}
	if durationMS.Valid {
		duration := durationMS.Int64
		value.DurationMS = &duration
	}
	value.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return runmodel.Validation{}, fmt.Errorf("parse validation creation time: %w", err)
	}

	return value, nil
}

const taskQuery = `
SELECT id,project_id,number,revision,status,title,description,acceptance_criteria,notes,
       created_at,updated_at,deleted_at
FROM tasks `

func scanTask(row scanner) (task.Task, error) {
	var value task.Task
	var createdAt, updatedAt string
	var deletedAt sql.NullString

	err := row.Scan(
		&value.ID,
		&value.ProjectID,
		&value.Number,
		&value.Revision,
		&value.Status,
		&value.Title,
		&value.Description,
		&value.AcceptanceCriteria,
		&value.Notes,
		&createdAt,
		&updatedAt,
		&deletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, runmodel.NewError(runmodel.CodeNotFound, "task was not found")
	}
	if err != nil {
		return task.Task{}, fmt.Errorf("scan task for run: %w", err)
	}

	value.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse task creation time: %w", err)
	}
	value.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse task update time: %w", err)
	}
	if deletedAt.Valid {
		parsed, err := parseTime(deletedAt.String)
		if err != nil {
			return task.Task{}, fmt.Errorf("parse task deletion time: %w", err)
		}
		value.DeletedAt = &parsed
	}

	return value, nil
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func stamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableString(value *string) any {
	if value == nil || *value == "" {
		return nil
	}

	return *value
}

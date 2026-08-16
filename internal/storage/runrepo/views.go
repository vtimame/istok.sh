package runrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	runmodel "s26.dev/istok-cli/internal/run"
)

func (r *Repository) GetRun(ctx context.Context, id string) (runmodel.Run, error) {
	if !runmodel.IsUUIDv7(id) {
		return runmodel.Run{}, runmodel.NewError(runmodel.CodeInvalid, "run id must be a canonical UUIDv7")
	}

	return scanRun(r.db.QueryRowContext(ctx, runQuery+`WHERE r.id=?`, id))
}

func (r *Repository) HasActiveRun(ctx context.Context, taskID string) (bool, error) {
	if !runmodel.IsUUIDv7(taskID) {
		return false, runmodel.NewError(runmodel.CodeInvalid, "task id must be a canonical UUIDv7")
	}

	var exists int
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE task_id=? AND status='active')`, taskID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check active run: %w", err)
	}

	return exists != 0, nil
}

func (r *Repository) ListRuns(ctx context.Context, options runmodel.ListOptions) ([]runmodel.Run, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	query := runQuery
	where := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if options.ProjectID != "" {
		query += " JOIN tasks t ON t.id=r.task_id"
		where = append(where, "t.project_id=?")
		args = append(args, options.ProjectID)
	}
	if options.TaskID != "" {
		where = append(where, "r.task_id=?")
		args = append(args, options.TaskID)
	}
	if len(options.Statuses) > 0 {
		placeholders := make([]string, len(options.Statuses))
		for i, status := range options.Statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		where = append(where, "r.status IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY r.started_at DESC,r.id DESC"
	if options.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, options.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	result := make([]runmodel.Run, 0)
	for rows.Next() {
		value, err := scanRun(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}

	return result, nil
}

func (r *Repository) ShowRun(ctx context.Context, id string) (runmodel.Show, error) {
	value, err := r.GetRun(ctx, id)
	if err != nil {
		return runmodel.Show{}, err
	}

	snapshot, err := r.getSnapshot(ctx, value.ContextSnapshotID)
	if err != nil {
		return runmodel.Show{}, err
	}
	executions, err := r.listExecutions(ctx, value.ID)
	if err != nil {
		return runmodel.Show{}, err
	}
	validations, err := r.listValidations(ctx, value.ID)
	if err != nil {
		return runmodel.Show{}, err
	}
	artifacts, err := r.listRunArtifacts(ctx, value.ID)
	if err != nil {
		return runmodel.Show{}, err
	}

	return runmodel.Show{
		Run:         value,
		Snapshot:    snapshot,
		Executions:  executions,
		Validations: validations,
		Artifacts:   artifacts,
	}, nil
}

func (r *Repository) getSnapshot(ctx context.Context, id string) (runmodel.ContextSnapshot, error) {
	var value runmodel.ContextSnapshot
	value.Records = make([]runmodel.ContextSnapshotItem, 0)
	var generatedAt, createdAt string

	err := r.db.QueryRowContext(ctx, `
		SELECT id,schema_version,project_id,generated_at,created_at
		FROM context_snapshots
		WHERE id=?
	`, id).Scan(&value.ID, &value.SchemaVersion, &value.ProjectID, &generatedAt, &createdAt)
	if err == sql.ErrNoRows {
		return runmodel.ContextSnapshot{}, runmodel.NewError(runmodel.CodeNotFound, "context snapshot was not found")
	}
	if err != nil {
		return runmodel.ContextSnapshot{}, fmt.Errorf("get context snapshot: %w", err)
	}

	value.GeneratedAt, err = parseTime(generatedAt)
	if err != nil {
		return runmodel.ContextSnapshot{}, fmt.Errorf("parse context snapshot generation time: %w", err)
	}
	value.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return runmodel.ContextSnapshot{}, fmt.Errorf("parse context snapshot creation time: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT record_id,record_revision,content_hash,kind,source,visibility,sensitivity,
		       title,body,snippet,tags
		FROM context_snapshot_items
		WHERE snapshot_id=?
		ORDER BY position
	`, id)
	if err != nil {
		return runmodel.ContextSnapshot{}, fmt.Errorf("list context snapshot items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item runmodel.ContextSnapshotItem
		var tags string

		if err := rows.Scan(
			&item.RecordID,
			&item.RecordRevision,
			&item.ContentHash,
			&item.Kind,
			&item.Source,
			&item.Visibility,
			&item.Sensitivity,
			&item.Title,
			&item.Body,
			&item.Snippet,
			&tags,
		); err != nil {
			return runmodel.ContextSnapshot{}, fmt.Errorf("scan context snapshot item: %w", err)
		}
		if err := json.Unmarshal([]byte(tags), &item.Tags); err != nil {
			return runmodel.ContextSnapshot{}, fmt.Errorf("decode context snapshot tags: %w", err)
		}

		value.Records = append(value.Records, item)
	}
	if err := rows.Err(); err != nil {
		return runmodel.ContextSnapshot{}, fmt.Errorf("iterate context snapshot items: %w", err)
	}

	return value, nil
}

func (r *Repository) listExecutions(ctx context.Context, runID string) ([]runmodel.Execution, error) {
	rows, err := r.db.QueryContext(ctx, executionQuery+`WHERE run_id=? ORDER BY started_at,id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()

	result := make([]runmodel.Execution, 0)
	for rows.Next() {
		value, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate executions: %w", err)
	}

	return result, nil
}

func (r *Repository) listValidations(ctx context.Context, runID string) ([]runmodel.Validation, error) {
	rows, err := r.db.QueryContext(ctx, validationQuery+`
		JOIN executions e ON e.id=v.execution_id
		WHERE e.run_id=?
		ORDER BY v.created_at,v.id
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list validations: %w", err)
	}
	defer rows.Close()

	result := make([]runmodel.Validation, 0)
	for rows.Next() {
		value, err := scanValidation(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate validations: %w", err)
	}

	return result, nil
}

package runrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	runmodel "s26.dev/istok-cli/internal/run"
)

const artifactQuery = `SELECT id,run_id,execution_id,kind,relative_path,sha256,original_size,stored_size,truncated,media_type,actor_id,actor_kind,actor_name,created_at FROM artifacts `

func scanArtifact(row scanner) (runmodel.Artifact, error) {
	var value runmodel.Artifact
	var truncated int
	var createdAt string
	err := row.Scan(&value.ID, &value.RunID, &value.ExecutionID, &value.Kind, &value.RelativePath, &value.SHA256, &value.OriginalSize, &value.StoredSize, &truncated, &value.MediaType, &value.Actor.ID, &value.Actor.Kind, &value.Actor.Name, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return runmodel.Artifact{}, runmodel.NewError(runmodel.CodeNotFound, "artifact was not found")
	}
	if err != nil {
		return runmodel.Artifact{}, fmt.Errorf("scan artifact: %w", err)
	}
	value.Truncated = truncated != 0
	value.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return runmodel.Artifact{}, fmt.Errorf("parse artifact creation time: %w", err)
	}
	return value, nil
}

func (r *Repository) GetArtifact(ctx context.Context, id string) (runmodel.Artifact, error) {
	if !runmodel.IsUUIDv7(id) {
		return runmodel.Artifact{}, runmodel.NewError(runmodel.CodeInvalid, "artifact id must be a canonical UUIDv7")
	}
	return scanArtifact(r.db.QueryRowContext(ctx, artifactQuery+`WHERE id=?`, id))
}

func (r *Repository) ListArtifacts(ctx context.Context, executionID string) ([]runmodel.Artifact, error) {
	if !runmodel.IsUUIDv7(executionID) {
		return nil, runmodel.NewError(runmodel.CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	rows, err := r.db.QueryContext(ctx, artifactQuery+`WHERE execution_id=? ORDER BY created_at,id`, executionID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	result := make([]runmodel.Artifact, 0)
	for rows.Next() {
		value, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifacts: %w", err)
	}
	return result, nil
}

func (r *Repository) listArtifacts(ctx context.Context, executionID string) ([]runmodel.Artifact, error) {
	return r.ListArtifacts(ctx, executionID)
}

func (r *Repository) listRunArtifacts(ctx context.Context, runID string) ([]runmodel.Artifact, error) {
	rows, err := r.db.QueryContext(ctx, artifactQuery+`WHERE run_id=? ORDER BY created_at,id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list run artifacts: %w", err)
	}
	defer rows.Close()
	result := make([]runmodel.Artifact, 0)
	for rows.Next() {
		value, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run artifacts: %w", err)
	}
	return result, nil
}

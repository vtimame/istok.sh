package runrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	runmodel "s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

func (r *Repository) Claim(ctx context.Context, input runmodel.ClaimRecord) (runmodel.Run, error) {
	if !runmodel.IsUUIDv7(input.RunID) || !runmodel.IsUUIDv7(input.TaskID) || !runmodel.IsUUIDv7(input.EventID) {
		return runmodel.Run{}, runmodel.NewError(runmodel.CodeInvalid, "run, task, and event ids must be canonical UUIDv7")
	}
	if err := input.Snapshot.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	if err := input.Actor.Validate(); err != nil {
		return runmodel.Run{}, err
	}
	if input.LeaseID == "" {
		input.LeaseID = input.RunID
	}
	if input.LeaseDuration <= 0 {
		input.LeaseDuration = 15 * time.Minute
	}
	if !runmodel.IsUUIDv7(input.LeaseID) {
		return runmodel.Run{}, runmodel.NewError(runmodel.CodeInvalid, "lease id must be a canonical UUIDv7")
	}

	err := r.write(ctx, "claim task", func(conn *sql.Conn) error {
		current, err := scanTask(conn.QueryRowContext(ctx, taskQuery+`WHERE id=?`, input.TaskID))
		if err != nil {
			return err
		}
		if current.DeletedAt != nil || current.Status != task.StatusOpen {
			return runmodel.NewError(runmodel.CodeInvalidTransition, "task is not claimable")
		}
		if current.ProjectID != input.Snapshot.ProjectID {
			return runmodel.NewError(runmodel.CodeConflict, "context snapshot belongs to another project")
		}

		var blockers int
		err = conn.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM task_dependencies d
			JOIN tasks blocker ON blocker.id=d.blocker_task_id
			WHERE d.blocked_task_id=?
			  AND blocker.deleted_at IS NULL
			  AND blocker.status!='done'
		`, input.TaskID).Scan(&blockers)
		if err != nil {
			return fmt.Errorf("check claim blockers: %w", err)
		}
		if blockers > 0 {
			return runmodel.NewError(runmodel.CodeConflict, "task has active blockers")
		}

		var active int
		err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE task_id=? AND status='active'`, input.TaskID).Scan(&active)
		if err != nil {
			return fmt.Errorf("check active run: %w", err)
		}
		if active > 0 {
			return runmodel.NewError(runmodel.CodeActiveRunExists, "task already has an active run")
		}

		now := r.now().UTC()
		if err := insertSnapshot(ctx, conn, input.Snapshot, now); err != nil {
			return err
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO runs (
				id,task_id,context_snapshot_id,revision,status,
				actor_id,actor_kind,actor_name,base_branch,base_commit,
				started_at,updated_at
			) VALUES (?,?,?,1,'active',?,?,?,?,?,?,?)
		`,
			input.RunID,
			input.TaskID,
			input.Snapshot.ID,
			input.Actor.ID,
			input.Actor.Kind,
			input.Actor.Name,
			input.BaseBranch,
			input.BaseCommit,
			stamp(now),
			stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "run conflicts with existing data")
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO run_leases (run_id,lease_id,owner_id,owner_kind,owner_name,heartbeat_at,expires_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?)
		`, input.RunID, input.LeaseID, input.Actor.ID, input.Actor.Kind, input.Actor.Name, stamp(now), stamp(now.Add(input.LeaseDuration)), stamp(now))
		if err != nil {
			return mapSQLError(err, "run lease conflicts with existing data")
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO task_events (
				id,task_id,type,body,task_revision,
				actor_id,actor_kind,actor_name,created_at
			) VALUES (?,?,'claimed',?,?,?,?,?,?)
		`,
			input.EventID,
			input.TaskID,
			input.RunID,
			current.Revision,
			input.Actor.ID,
			input.Actor.Kind,
			input.Actor.Name,
			stamp(now),
		)
		if err != nil {
			return mapSQLError(err, "claim event conflicts with existing data")
		}

		return nil
	})
	if err != nil {
		return runmodel.Run{}, err
	}

	return r.GetRun(ctx, input.RunID)
}

func insertSnapshot(ctx context.Context, conn *sql.Conn, snapshot runmodel.ContextSnapshot, createdAt time.Time) error {
	_, err := conn.ExecContext(ctx, `
		INSERT INTO context_snapshots (id,schema_version,project_id,generated_at,created_at)
		VALUES (?,?,?,?,?)
	`, snapshot.ID, snapshot.SchemaVersion, snapshot.ProjectID, stamp(snapshot.GeneratedAt), stamp(createdAt))
	if err != nil {
		return mapSQLError(err, "context snapshot conflicts with existing data")
	}

	for position, item := range snapshot.Records {
		tags, err := json.Marshal(item.Tags)
		if err != nil {
			return fmt.Errorf("encode context snapshot tags: %w", err)
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO context_snapshot_items (
				snapshot_id,position,record_id,record_revision,content_hash,
				kind,source,visibility,sensitivity,title,body,snippet,tags
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		`,
			snapshot.ID,
			position,
			item.RecordID,
			item.RecordRevision,
			item.ContentHash,
			item.Kind,
			item.Source,
			item.Visibility,
			item.Sensitivity,
			item.Title,
			item.Body,
			item.Snippet,
			string(tags),
		)
		if err != nil {
			return mapSQLError(err, "context snapshot item conflicts with existing data")
		}
	}

	return nil
}

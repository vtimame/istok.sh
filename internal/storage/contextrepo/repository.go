package contextrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const contextRecordQuery = `
SELECT cr.id,cr.project_id,cr.revision,cr.kind,cr.title,cr.body,cr.tags,cr.source,cr.visibility,cr.sensitivity,cr.enabled,cr.priority,cr.scope,cr.delivery,cr.review_after,cr.expires_at,cr.superseded_by,cr.actor_id,cr.actor_kind,cr.actor_name,cr.created_at,cr.updated_at,cr.deleted_at
FROM context_records cr `

func (r *Repository) Create(ctx context.Context, input contextmodel.CreateInput, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if err := input.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	if err := actor.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}

	id := input.ID
	if id == "" {
		generated, err := contextmodel.NewID()
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("generate context id: %w", err)
		}
		id = generated
	}

	value := contextmodel.ProjectContextRecord{
		ID:           id,
		ProjectID:    input.ProjectID,
		Kind:         input.Kind,
		Title:        strings.TrimSpace(input.Title),
		Body:         input.Body,
		Source:       input.Source,
		Visibility:   input.Visibility,
		Sensitivity:  input.Sensitivity,
		Delivery:     input.Delivery,
		Actor:        actor,
		ReviewAfter:  cloneTime(input.ReviewAfter),
		ExpiresAt:    cloneTime(input.ExpiresAt),
		SupersededBy: cloneString(input.SupersededBy),
	}
	if value.Delivery == "" {
		value.Delivery = contextmodel.DefaultDelivery(value.Kind)
	}
	normalizePolicy(&value, input.Enabled, input.Priority, input.Scope)

	tags, err := contextmodel.NormalizeTags(input.Tags)
	if err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	value.Tags = tags

	return r.write(ctx, func(conn *sql.Conn) (contextmodel.ProjectContextRecord, error) {
		if err := requireActiveProject(ctx, conn, value.ProjectID); err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}
		if err := validateSupersession(ctx, conn, value); err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		now := time.Now().UTC()
		marshaledTags, err := contextmodel.MarshalTags(value.Tags)
		if err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		value.Revision = 1
		value.CreatedAt = now
		value.UpdatedAt = now

		_, err = conn.ExecContext(
			ctx,
			`INSERT INTO context_records(
			id,project_id,revision,kind,title,body,tags,source,visibility,sensitivity,enabled,priority,scope,delivery,review_after,expires_at,superseded_by,actor_id,actor_kind,actor_name,created_at,updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			value.ID,
			value.ProjectID,
			value.Revision,
			value.Kind,
			value.Title,
			value.Body,
			marshaledTags,
			value.Source,
			value.Visibility,
			value.Sensitivity,
			policyEnabled(value),
			policyPriority(value),
			policyScope(value),
			value.Delivery,
			nullableTime(value.ReviewAfter),
			nullableTime(value.ExpiresAt),
			nullableString(value.SupersededBy),
			value.Actor.ID,
			value.Actor.Kind,
			value.Actor.Name,
			stamp(now),
			stamp(now),
		)
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("insert context record: %w", mapSQLError(err))
		}

		if err := insertEvent(ctx, conn, value.ID, "created", "", value.Revision, actor, now); err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		return value, nil
	})
}

func (r *Repository) Get(ctx context.Context, id string, includeDeleted bool) (contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(id) {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id must be a canonical UUIDv7")
	}

	where := "WHERE cr.id=?"
	if !includeDeleted {
		where += " AND deleted_at IS NULL"
	}

	return scanContextRecord(r.db.QueryRowContext(ctx, contextRecordQuery+where, id))
}

func (r *Repository) List(ctx context.Context, projectID string, options contextmodel.ListOptions) ([]contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(projectID) {
		return nil, contextmodel.NewError(contextmodel.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	query := contextRecordQuery + "WHERE cr.project_id=?"
	args := []any{projectID}
	if !options.IncludeDeleted {
		query += " AND cr.deleted_at IS NULL"
	}
	if !options.IncludeDisabled {
		query += " AND (cr.kind != 'instruction' OR cr.enabled = 1)"
	}
	if len(options.Kinds) > 0 {
		query += " AND cr.kind IN ("
		for i, kind := range options.Kinds {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, kind)
		}
		query += ")"
	}
	if len(options.Sources) > 0 {
		query += " AND cr.source IN ("
		for i, value := range options.Sources {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, value)
		}
		query += ")"
	}
	if len(options.Visibilities) > 0 {
		query += " AND cr.visibility IN ("
		for i, value := range options.Visibilities {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, value)
		}
		query += ")"
	}
	if len(options.Sensitivities) > 0 {
		query += " AND cr.sensitivity IN ("
		for i, value := range options.Sensitivities {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, value)
		}
		query += ")"
	}
	query += " ORDER BY cr.updated_at DESC, cr.id"
	if options.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", options.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list context records: %w", err)
	}
	defer rows.Close()

	var values []contextmodel.ProjectContextRecord
	for rows.Next() {
		value, err := scanContextRecord(rows)
		if err != nil {
			return nil, err
		}

		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context records: %w", err)
	}
	return values, nil
}

func (r *Repository) Search(ctx context.Context, projectID string, options contextmodel.SearchOptions) ([]contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(projectID) {
		return nil, contextmodel.NewError(contextmodel.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	query := contextRecordQuery + "WHERE cr.project_id=?"
	args := []any{projectID}
	if !options.IncludeDeleted {
		query += " AND cr.deleted_at IS NULL"
	}
	if !options.IncludeDisabled {
		query += " AND (cr.kind != 'instruction' OR cr.enabled = 1)"
	}
	query += " AND (LOWER(cr.title) LIKE ? OR LOWER(cr.body) LIKE ? OR LOWER(cr.tags) LIKE ?)"
	search := strings.ToLower(strings.TrimSpace(options.Query))
	pattern := "%" + search + "%"
	args = append(args, pattern, pattern, pattern)
	query += " ORDER BY cr.updated_at DESC, cr.id"
	if options.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", options.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search context records: %w", err)
	}
	defer rows.Close()

	var values []contextmodel.ProjectContextRecord
	for rows.Next() {
		value, err := scanContextRecord(rows)
		if err != nil {
			return nil, err
		}

		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context search rows: %w", err)
	}

	return values, nil
}

func (r *Repository) Events(ctx context.Context, id string) ([]contextmodel.ContextEvent, error) {
	if !contextmodel.IsUUIDv7(id) {
		return nil, contextmodel.NewError(contextmodel.CodeInvalid, "context record id must be a canonical UUIDv7")
	}
	if err := r.requireRecord(ctx, id); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `SELECT id,record_id,type,body,record_revision,actor_id,actor_kind,actor_name,created_at FROM context_events WHERE record_id=? ORDER BY created_at,rowid`, id)
	if err != nil {
		return nil, fmt.Errorf("list context events: %w", err)
	}
	defer rows.Close()

	var values []contextmodel.ContextEvent
	for rows.Next() {
		value, err := scanContextEvent(rows)
		if err != nil {
			return nil, err
		}

		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context events: %w", err)
	}

	return values, nil
}

func (r *Repository) Update(ctx context.Context, id string, expected int64, patch contextmodel.Patch, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(id) || expected < 1 {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id and expected revision are required")
	}
	if err := patch.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	if err := actor.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}

	return r.mutate(ctx, id, expected, actor, "updated", "", patch)
}

func (r *Repository) Archive(ctx context.Context, id string, expected int64, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(id) || expected < 1 {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id and expected revision are required")
	}
	if err := actor.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}

	return r.changeDeleted(ctx, id, expected, actor, true)
}

func (r *Repository) Restore(ctx context.Context, id string, expected int64, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(id) || expected < 1 {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id and expected revision are required")
	}
	if err := actor.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}

	return r.changeDeleted(ctx, id, expected, actor, false)
}

func (r *Repository) mutate(ctx context.Context, id string, expected int64, actor contextmodel.ActorSnapshot, event, body string, patch contextmodel.Patch) (contextmodel.ProjectContextRecord, error) {
	return r.write(ctx, func(conn *sql.Conn) (contextmodel.ProjectContextRecord, error) {
		value, err := scanContextRecord(conn.QueryRowContext(ctx, contextRecordQuery+`WHERE cr.id=?`, id))
		if err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}
		if value.Revision != expected {
			return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeRevisionConflict, "context record revision does not match expected_revision")
		}
		if err = patch.Apply(&value); err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}
		if err = validateSupersession(ctx, conn, value); err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		marshaledTags, err := contextmodel.MarshalTags(value.Tags)
		if err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		now := time.Now().UTC()
		result, err := conn.ExecContext(
			ctx,
			`UPDATE context_records SET kind=?,title=?,body=?,tags=?,source=?,visibility=?,sensitivity=?,enabled=?,priority=?,scope=?,delivery=?,review_after=?,expires_at=?,superseded_by=?,actor_id=?,actor_kind=?,actor_name=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`,
			value.Kind,
			value.Title,
			value.Body,
			marshaledTags,
			value.Source,
			value.Visibility,
			value.Sensitivity,
			policyEnabled(value),
			policyPriority(value),
			policyScope(value),
			value.Delivery,
			nullableTime(value.ReviewAfter),
			nullableTime(value.ExpiresAt),
			nullableString(value.SupersededBy),
			actor.ID,
			actor.Kind,
			actor.Name,
			stamp(now),
			id,
			expected,
		)
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("update context record: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("check context record update: %w", err)
		}
		if changed != 1 {
			return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeRevisionConflict, "context record revision does not match expected_revision")
		}

		value, err = scanContextRecord(conn.QueryRowContext(ctx, contextRecordQuery+`WHERE cr.id=?`, id))
		if err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		if err := insertEvent(ctx, conn, id, event, body, value.Revision, actor, now); err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		return value, nil
	})
}

func (r *Repository) requireRecord(ctx context.Context, id string) error {
	var deleted sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT deleted_at FROM context_records WHERE id=?`, id).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return contextmodel.NewError(contextmodel.CodeNotFound, "context record was not found")
	}
	if err != nil {
		return fmt.Errorf("get context record: %w", err)
	}

	return nil
}

func (r *Repository) changeDeleted(ctx context.Context, id string, expected int64, actor contextmodel.ActorSnapshot, deleted bool) (contextmodel.ProjectContextRecord, error) {
	return r.write(ctx, func(conn *sql.Conn) (contextmodel.ProjectContextRecord, error) {
		value, err := scanContextRecord(conn.QueryRowContext(ctx, contextRecordQuery+"WHERE cr.id=?", id))
		if err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		if (value.DeletedAt != nil) == deleted {
			return value, nil
		}

		if value.Revision != expected {
			return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeRevisionConflict, "context record revision does not match expected_revision")
		}

		now := time.Now().UTC()
		var result sql.Result
		if deleted {
			result, err = conn.ExecContext(ctx, `UPDATE context_records SET deleted_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND deleted_at IS NULL`, stamp(now), stamp(now), id, expected)
		} else {
			result, err = conn.ExecContext(ctx, `UPDATE context_records SET deleted_at=NULL,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND deleted_at IS NOT NULL`, stamp(now), id, expected)
		}
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("change context record deleted state: %w", err)
		}

		changed, err := result.RowsAffected()
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("check context record deleted state: %w", err)
		}
		if changed != 1 {
			return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeRevisionConflict, "context record revision does not match expected_revision")
		}

		value, err = scanContextRecord(conn.QueryRowContext(ctx, contextRecordQuery+"WHERE cr.id=?", id))
		if err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		eventType := "archived"
		if !deleted {
			eventType = "restored"
		}

		if err := insertEvent(ctx, conn, id, eventType, "", value.Revision, actor, now); err != nil {
			return contextmodel.ProjectContextRecord{}, err
		}

		return value, nil
	})
}

func scanContextRecord(row interface{ Scan(...any) error }) (contextmodel.ProjectContextRecord, error) {
	var value contextmodel.ProjectContextRecord
	var tagsRaw string
	var createdRaw string
	var updatedRaw string
	var deleted sql.NullString
	var enabled int
	var priority contextmodel.Priority
	var scope contextmodel.Scope
	var delivery contextmodel.Delivery
	var reviewAfter sql.NullString
	var expiresAt sql.NullString
	var supersededBy sql.NullString

	err := row.Scan(
		&value.ID,
		&value.ProjectID,
		&value.Revision,
		&value.Kind,
		&value.Title,
		&value.Body,
		&tagsRaw,
		&value.Source,
		&value.Visibility,
		&value.Sensitivity,
		&enabled,
		&priority,
		&scope,
		&delivery,
		&reviewAfter,
		&expiresAt,
		&supersededBy,
		&value.Actor.ID,
		&value.Actor.Kind,
		&value.Actor.Name,
		&createdRaw,
		&updatedRaw,
		&deleted,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeNotFound, "context record was not found")
	}
	if err != nil {
		return contextmodel.ProjectContextRecord{}, fmt.Errorf("scan context record: %w", err)
	}

	if err := json.Unmarshal([]byte(tagsRaw), &value.Tags); err != nil {
		return contextmodel.ProjectContextRecord{}, fmt.Errorf("decode context tags: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return contextmodel.ProjectContextRecord{}, fmt.Errorf("parse context creation time: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedRaw)
	if err != nil {
		return contextmodel.ProjectContextRecord{}, fmt.Errorf("parse context update time: %w", err)
	}
	value.CreatedAt = createdAt
	value.UpdatedAt = updatedAt
	value.Delivery = delivery
	if value.Kind == contextmodel.KindInstruction {
		value.Enabled = boolPtr(enabled != 0)
		value.Priority = priorityPtr(priority)
		value.Scope = scopePtr(scope)
	}

	if deleted.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, deleted.String)
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("parse context deletion time: %w", err)
		}
		value.DeletedAt = &parsed
	}
	if reviewAfter.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, reviewAfter.String)
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("parse context review time: %w", err)
		}
		value.ReviewAfter = &parsed
	}
	if expiresAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, expiresAt.String)
		if err != nil {
			return contextmodel.ProjectContextRecord{}, fmt.Errorf("parse context expiry time: %w", err)
		}
		value.ExpiresAt = &parsed
	}
	if supersededBy.Valid {
		value.SupersededBy = &supersededBy.String
	}

	return value, nil
}

func normalizePolicy(value *contextmodel.ProjectContextRecord, enabled *bool, priority *contextmodel.Priority, scope *contextmodel.Scope) {
	if value.Kind != contextmodel.KindInstruction {
		value.Enabled, value.Priority, value.Scope = nil, nil, nil
		return
	}
	if enabled == nil {
		enabled = boolPtr(true)
	}
	if priority == nil {
		priority = priorityPtr(contextmodel.PriorityNormal)
	}
	if scope == nil {
		scope = scopePtr(contextmodel.ScopeProject)
	}
	value.Enabled, value.Priority, value.Scope = enabled, priority, scope
}

func policyEnabled(value contextmodel.ProjectContextRecord) int {
	if value.Kind == contextmodel.KindInstruction && value.Enabled != nil && !*value.Enabled {
		return 0
	}
	return 1
}

func policyPriority(value contextmodel.ProjectContextRecord) contextmodel.Priority {
	if value.Kind == contextmodel.KindInstruction && value.Priority != nil {
		return *value.Priority
	}
	return contextmodel.PriorityNormal
}

func policyScope(value contextmodel.ProjectContextRecord) contextmodel.Scope {
	if value.Kind == contextmodel.KindInstruction && value.Scope != nil {
		return *value.Scope
	}
	return contextmodel.ScopeProject
}

func validateSupersession(ctx context.Context, conn *sql.Conn, value contextmodel.ProjectContextRecord) error {
	if value.SupersededBy == nil {
		return nil
	}
	if *value.SupersededBy == value.ID {
		return contextmodel.NewError(contextmodel.CodeInvalid, "context record cannot supersede itself")
	}

	var projectID string
	err := conn.QueryRowContext(ctx, `SELECT project_id FROM context_records WHERE id=? AND deleted_at IS NULL`, *value.SupersededBy).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return contextmodel.NewError(contextmodel.CodeNotFound, "superseding context record was not found")
	}
	if err != nil {
		return fmt.Errorf("validate superseding context record: %w", err)
	}
	if projectID != value.ProjectID {
		return contextmodel.NewError(contextmodel.CodeConflict, "superseding context record belongs to another project")
	}

	return nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}

	return stamp(value.UTC())
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}

	return *value
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	copy := value.UTC()
	return &copy
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}

	copy := strings.TrimSpace(*value)
	return &copy
}

func boolPtr(value bool) *bool                                       { return &value }
func priorityPtr(value contextmodel.Priority) *contextmodel.Priority { return &value }
func scopePtr(value contextmodel.Scope) *contextmodel.Scope          { return &value }

func scanContextEvent(row interface{ Scan(...any) error }) (contextmodel.ContextEvent, error) {
	var value contextmodel.ContextEvent
	var createdRaw string

	err := row.Scan(&value.ID, &value.RecordID, &value.Type, &value.Body, &value.RecordRevision, &value.Actor.ID, &value.Actor.Kind, &value.Actor.Name, &createdRaw)
	if err != nil {
		return contextmodel.ContextEvent{}, fmt.Errorf("scan context event: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return contextmodel.ContextEvent{}, fmt.Errorf("parse context event time: %w", err)
	}
	value.CreatedAt = createdAt

	return value, nil
}

func insertEvent(ctx context.Context, conn *sql.Conn, recordID, kind, body string, revision int64, actor contextmodel.ActorSnapshot, now time.Time) error {
	id, err := contextmodel.NewID()
	if err != nil {
		return fmt.Errorf("generate context event ID: %w", err)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO context_events(id,record_id,type,body,record_revision,actor_id,actor_kind,actor_name,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, recordID, kind, body, revision, actor.ID, actor.Kind, actor.Name, stamp(now))
	if err != nil {
		return fmt.Errorf("insert context event: %w", mapSQLError(err))
	}

	return nil
}

func stamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func requireActiveProject(ctx context.Context, conn *sql.Conn, projectID string) error {
	var deleted sql.NullString
	err := conn.QueryRowContext(ctx, `SELECT deleted_at FROM projects WHERE id=?`, projectID).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return contextmodel.NewError(contextmodel.CodeConflict, "project was not found")
	}
	if err != nil {
		return fmt.Errorf("get project for context create: %w", err)
	}
	if deleted.Valid {
		return contextmodel.NewError(contextmodel.CodeConflict, "cannot create context record in a deleted project")
	}

	return nil
}

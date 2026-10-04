package knowledgerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
)

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db, now: time.Now}
}

const itemQuery = `
SELECT id,project_id,revision,kind,status,title,summary,body,tags,visibility,sensitivity,
       content_hash,provenance,reviewed_at,superseded_by,actor_id,actor_kind,actor_name,created_at,updated_at
FROM knowledge_items `

const catalogQuery = `
SELECT id,project_id,revision,kind,status,title,summary,substr(trim(body),1,280),tags,visibility,sensitivity,
	   content_hash,reviewed_at,superseded_by,updated_at
FROM knowledge_items `

func (r *Repository) Create(ctx context.Context, input knowledge.CreateInput, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	if err := input.Validate(); err != nil {
		return knowledge.Item{}, err
	}
	if err := actor.Validate(); err != nil {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "%s", err.Error())
	}

	id := input.ID
	if id == "" {
		var err error
		id, err = knowledge.NewID()
		if err != nil {
			return knowledge.Item{}, fmt.Errorf("generate knowledge id: %w", err)
		}
	}
	eventID, err := knowledge.NewID()
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("generate knowledge event id: %w", err)
	}
	tags, _ := knowledge.NormalizeTags(input.Tags)
	provenance, _ := knowledge.NormalizeProvenance(input.Provenance)
	now := r.now().UTC()
	item := knowledge.Item{
		ID: id, ProjectID: input.ProjectID, Revision: 1, Kind: input.Kind, Status: knowledge.StatusDraft,
		Title: strings.TrimSpace(input.Title), Summary: strings.TrimSpace(input.Summary), Body: input.Body,
		Tags: tags, Visibility: input.Visibility, Sensitivity: input.Sensitivity, Provenance: provenance,
		Actor: actor, CreatedAt: now, UpdatedAt: now,
	}
	item.ContentHash = knowledge.ContentHash(item)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("begin knowledge create: %w", err)
	}
	defer tx.Rollback()
	if err := requireActiveProject(ctx, tx, item.ProjectID); err != nil {
		return knowledge.Item{}, err
	}
	encodedTags, _ := json.Marshal(item.Tags)
	encodedProvenance, _ := json.Marshal(item.Provenance)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO knowledge_items(
			id,project_id,revision,kind,status,title,summary,body,tags,visibility,sensitivity,
			content_hash,provenance,actor_id,actor_kind,actor_name,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.ProjectID, item.Revision, item.Kind, item.Status, item.Title, item.Summary, item.Body,
		string(encodedTags), item.Visibility, item.Sensitivity, item.ContentHash, string(encodedProvenance),
		actor.ID, actor.Kind, actor.Name, stamp(now), stamp(now),
	)
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("insert knowledge item: %w", mapSQLError(err))
	}
	if err := insertEvent(ctx, tx, eventID, item.ID, "created", "", item.Revision, actor, now); err != nil {
		return knowledge.Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Item{}, fmt.Errorf("commit knowledge create: %w", err)
	}

	return item, nil
}

func (r *Repository) Get(ctx context.Context, id string) (knowledge.Item, error) {
	if !knowledge.IsUUIDv7(id) {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "knowledge item id must be a canonical UUIDv7")
	}

	return scanItem(r.db.QueryRowContext(ctx, itemQuery+"WHERE id=?", id))
}

func (r *Repository) Update(ctx context.Context, id string, expectedRevision int64, patch knowledge.Patch, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	if !knowledge.IsUUIDv7(id) || expectedRevision < 1 {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "knowledge id and expected revision are required")
	}
	if err := patch.Validate(); err != nil {
		return knowledge.Item{}, err
	}
	if err := actor.Validate(); err != nil {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "%s", err.Error())
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("begin knowledge update: %w", err)
	}
	defer tx.Rollback()
	item, err := scanItem(tx.QueryRowContext(ctx, itemQuery+"WHERE id=?", id))
	if err != nil {
		return knowledge.Item{}, err
	}
	if item.Revision != expectedRevision {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeRevisionConflict, "knowledge revision conflict: expected %d, current %d", expectedRevision, item.Revision)
	}
	if err := patch.Apply(&item); err != nil {
		return knowledge.Item{}, err
	}
	item.Revision++
	item.Actor = actor
	item.UpdatedAt = r.now().UTC()
	if err := updateItem(ctx, tx, item, expectedRevision); err != nil {
		return knowledge.Item{}, err
	}
	eventID, err := knowledge.NewID()
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("generate knowledge event id: %w", err)
	}
	if err := insertEvent(ctx, tx, eventID, item.ID, "updated", "", item.Revision, actor, item.UpdatedAt); err != nil {
		return knowledge.Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Item{}, fmt.Errorf("commit knowledge update: %w", err)
	}

	return item, nil
}

func (r *Repository) Promote(ctx context.Context, id string, expectedRevision int64, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	return r.changeStatus(ctx, id, expectedRevision, knowledge.StatusCurrent, nil, "promoted", "", actor)
}

func (r *Repository) Review(ctx context.Context, id string, expectedRevision int64, note string, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	note = strings.TrimSpace(note)
	if !knowledge.IsUUIDv7(id) || expectedRevision < 1 || note == "" || len(note) > knowledge.MaxReviewNoteBytes {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "knowledge id, expected revision, and review note are required")
	}
	if err := actor.Validate(); err != nil {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "%s", err.Error())
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("begin knowledge review: %w", err)
	}
	defer tx.Rollback()
	item, err := scanItem(tx.QueryRowContext(ctx, itemQuery+"WHERE id=?", id))
	if err != nil {
		return knowledge.Item{}, err
	}
	if item.Revision != expectedRevision {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeRevisionConflict, "knowledge revision conflict: expected %d, current %d", expectedRevision, item.Revision)
	}
	if item.Status != knowledge.StatusDraft {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeConflict, "only draft knowledge can be reviewed")
	}

	now := r.now().UTC()
	item.ReviewedAt = &now
	item.Revision++
	item.Actor = actor
	item.UpdatedAt = now
	if err := updateItem(ctx, tx, item, expectedRevision); err != nil {
		return knowledge.Item{}, err
	}
	eventID, err := knowledge.NewID()
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("generate knowledge review event id: %w", err)
	}
	if err := insertEvent(ctx, tx, eventID, item.ID, "reviewed", note, item.Revision, actor, now); err != nil {
		return knowledge.Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Item{}, fmt.Errorf("commit knowledge review: %w", err)
	}

	return item, nil
}

func (r *Repository) Supersede(ctx context.Context, id string, expectedRevision int64, replacementID string, actor contextmodel.ActorSnapshot) (knowledge.Item, knowledge.Item, error) {
	if !knowledge.IsUUIDv7(id) || !knowledge.IsUUIDv7(replacementID) || id == replacementID || expectedRevision < 1 {
		return knowledge.Item{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "knowledge id, replacement id, and expected revision are required")
	}
	if err := actor.Validate(); err != nil {
		return knowledge.Item{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "%s", err.Error())
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return knowledge.Item{}, knowledge.Item{}, fmt.Errorf("begin knowledge supersession: %w", err)
	}
	defer tx.Rollback()
	item, err := scanItem(tx.QueryRowContext(ctx, itemQuery+"WHERE id=?", id))
	if err != nil {
		return knowledge.Item{}, knowledge.Item{}, err
	}
	replacement, err := scanItem(tx.QueryRowContext(ctx, itemQuery+"WHERE id=?", replacementID))
	if err != nil {
		return knowledge.Item{}, knowledge.Item{}, err
	}
	if item.ProjectID != replacement.ProjectID {
		return knowledge.Item{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeNotFound, "replacement knowledge was not found")
	}
	if item.Revision != expectedRevision {
		return knowledge.Item{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeRevisionConflict, "knowledge revision conflict: expected %d, current %d", expectedRevision, item.Revision)
	}
	if item.Status != knowledge.StatusCurrent {
		return knowledge.Item{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeConflict, "only current knowledge can be superseded")
	}
	if replacement.Status == knowledge.StatusSuperseded {
		return knowledge.Item{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeConflict, "superseded knowledge cannot be a replacement")
	}
	if replacement.Status == knowledge.StatusDraft && replacement.ReviewedAt == nil {
		return knowledge.Item{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeConflict, "replacement draft must be reviewed before supersession")
	}

	now := r.now().UTC()
	item.Status = knowledge.StatusSuperseded
	item.SupersededBy = &replacement.ID
	item.Revision++
	item.Actor = actor
	item.UpdatedAt = now
	if err := updateItem(ctx, tx, item, expectedRevision); err != nil {
		return knowledge.Item{}, knowledge.Item{}, err
	}

	replacementExpected := replacement.Revision
	if replacement.Status != knowledge.StatusCurrent {
		replacement.Status = knowledge.StatusCurrent
		replacement.Revision++
		replacement.Actor = actor
		replacement.UpdatedAt = now
		if err := updateItem(ctx, tx, replacement, replacementExpected); err != nil {
			return knowledge.Item{}, knowledge.Item{}, err
		}
	}
	oldEventID, err := knowledge.NewID()
	if err != nil {
		return knowledge.Item{}, knowledge.Item{}, fmt.Errorf("generate knowledge supersession event id: %w", err)
	}
	newEventID, err := knowledge.NewID()
	if err != nil {
		return knowledge.Item{}, knowledge.Item{}, fmt.Errorf("generate replacement promotion event id: %w", err)
	}
	if err := insertEvent(ctx, tx, oldEventID, item.ID, "superseded", replacement.ID, item.Revision, actor, now); err != nil {
		return knowledge.Item{}, knowledge.Item{}, err
	}
	if replacement.Revision != replacementExpected {
		if err := insertEvent(ctx, tx, newEventID, replacement.ID, "promoted", "supersedes "+item.ID, replacement.Revision, actor, now); err != nil {
			return knowledge.Item{}, knowledge.Item{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Item{}, knowledge.Item{}, fmt.Errorf("commit knowledge supersession: %w", err)
	}

	return item, replacement, nil
}

func (r *Repository) changeStatus(ctx context.Context, id string, expectedRevision int64, status knowledge.Status, supersededBy *string, eventType, eventBody string, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	if !knowledge.IsUUIDv7(id) || expectedRevision < 1 || !status.Valid() {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "knowledge id, expected revision, and status are required")
	}
	if err := actor.Validate(); err != nil {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "%s", err.Error())
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("begin knowledge lifecycle update: %w", err)
	}
	defer tx.Rollback()
	item, err := scanItem(tx.QueryRowContext(ctx, itemQuery+"WHERE id=?", id))
	if err != nil {
		return knowledge.Item{}, err
	}
	if item.Revision != expectedRevision {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeRevisionConflict, "knowledge revision conflict: expected %d, current %d", expectedRevision, item.Revision)
	}
	if status == knowledge.StatusCurrent && item.Status != knowledge.StatusDraft {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeConflict, "only draft knowledge can be promoted")
	}
	if status == knowledge.StatusCurrent && item.ReviewedAt == nil {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeConflict, "draft knowledge must be reviewed before promotion")
	}
	item.Status = status
	item.SupersededBy = supersededBy
	item.Revision++
	item.Actor = actor
	item.UpdatedAt = r.now().UTC()
	if err := updateItem(ctx, tx, item, expectedRevision); err != nil {
		return knowledge.Item{}, err
	}
	eventID, err := knowledge.NewID()
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("generate knowledge lifecycle event id: %w", err)
	}
	if err := insertEvent(ctx, tx, eventID, item.ID, eventType, eventBody, item.Revision, actor, item.UpdatedAt); err != nil {
		return knowledge.Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return knowledge.Item{}, fmt.Errorf("commit knowledge lifecycle update: %w", err)
	}

	return item, nil
}

func (r *Repository) Catalog(ctx context.Context, projectID string, options knowledge.CatalogOptions) ([]knowledge.CatalogItem, error) {
	if !knowledge.IsUUIDv7(projectID) {
		return nil, knowledge.NewError(knowledge.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	query := catalogQuery + "WHERE project_id=?"
	args := []any{projectID}
	query, args = appendFilters(query, args, "kind", kinds(options.Kinds))
	query, args = appendFilters(query, args, "status", statuses(options.Statuses))
	query, args = appendFilters(query, args, "visibility", visibilities(options.Visibilities))
	query, args = appendFilters(query, args, "sensitivity", sensitivities(options.Sensitivities))
	query += " ORDER BY updated_at DESC,id LIMIT ? OFFSET ?"
	args = append(args, options.EffectiveLimit(), options.Offset)

	return queryCatalog(ctx, r.db, query, args...)
}

func (r *Repository) Search(ctx context.Context, projectID string, options knowledge.SearchOptions) ([]knowledge.CatalogItem, error) {
	if !knowledge.IsUUIDv7(projectID) {
		return nil, knowledge.NewError(knowledge.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(options.Query)) + "%"
	query := catalogQuery + `WHERE project_id=? AND (
		LOWER(title) LIKE ? OR LOWER(summary) LIKE ? OR LOWER(body) LIKE ? OR LOWER(tags) LIKE ? OR LOWER(provenance) LIKE ?
	) ORDER BY CASE WHEN LOWER(title) LIKE ? THEN 0 WHEN LOWER(summary) LIKE ? THEN 1 ELSE 2 END,updated_at DESC,id LIMIT ? OFFSET ?`

	return queryCatalog(ctx, r.db, query, projectID, pattern, pattern, pattern, pattern, pattern, pattern, pattern, options.EffectiveLimit(), options.Offset)
}

func (r *Repository) Events(ctx context.Context, itemID string) ([]knowledge.Event, error) {
	if !knowledge.IsUUIDv7(itemID) {
		return nil, knowledge.NewError(knowledge.CodeInvalid, "knowledge item id must be a canonical UUIDv7")
	}
	if _, err := r.Get(ctx, itemID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id,item_id,type,body,item_revision,actor_id,actor_kind,actor_name,created_at
		FROM knowledge_events WHERE item_id=? ORDER BY created_at,id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("list knowledge events: %w", err)
	}
	defer rows.Close()
	var result []knowledge.Event
	for rows.Next() {
		var value knowledge.Event
		var createdAt string
		if err := rows.Scan(&value.ID, &value.ItemID, &value.Type, &value.Body, &value.ItemRevision, &value.Actor.ID, &value.Actor.Kind, &value.Actor.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("scan knowledge event: %w", err)
		}
		value.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func updateItem(ctx context.Context, tx *sql.Tx, item knowledge.Item, expectedRevision int64) error {
	tags, _ := json.Marshal(item.Tags)
	provenance, _ := json.Marshal(item.Provenance)
	result, err := tx.ExecContext(ctx, `UPDATE knowledge_items SET
		revision=?,kind=?,status=?,title=?,summary=?,body=?,tags=?,visibility=?,sensitivity=?,content_hash=?,provenance=?,reviewed_at=?,superseded_by=?,
		actor_id=?,actor_kind=?,actor_name=?,updated_at=? WHERE id=? AND revision=?`,
		item.Revision, item.Kind, item.Status, item.Title, item.Summary, item.Body, string(tags), item.Visibility, item.Sensitivity,
		item.ContentHash, string(provenance), nullableTime(item.ReviewedAt), nullableString(item.SupersededBy), item.Actor.ID, item.Actor.Kind, item.Actor.Name,
		stamp(item.UpdatedAt), item.ID, expectedRevision)
	if err != nil {
		return fmt.Errorf("update knowledge item: %w", mapSQLError(err))
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return knowledge.NewError(knowledge.CodeRevisionConflict, "knowledge revision conflict")
	}
	return nil
}

func scanItem(row interface{ Scan(...any) error }) (knowledge.Item, error) {
	var value knowledge.Item
	var tags, provenance, createdAt, updatedAt string
	var reviewedAt, supersededBy sql.NullString
	err := row.Scan(&value.ID, &value.ProjectID, &value.Revision, &value.Kind, &value.Status, &value.Title, &value.Summary, &value.Body,
		&tags, &value.Visibility, &value.Sensitivity, &value.ContentHash, &provenance, &reviewedAt, &supersededBy,
		&value.Actor.ID, &value.Actor.Kind, &value.Actor.Name, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeNotFound, "knowledge item was not found")
	}
	if err != nil {
		return knowledge.Item{}, fmt.Errorf("scan knowledge item: %w", err)
	}
	if err := json.Unmarshal([]byte(tags), &value.Tags); err != nil {
		return knowledge.Item{}, fmt.Errorf("decode knowledge tags: %w", err)
	}
	if err := json.Unmarshal([]byte(provenance), &value.Provenance); err != nil {
		return knowledge.Item{}, fmt.Errorf("decode knowledge provenance: %w", err)
	}
	if supersededBy.Valid {
		value.SupersededBy = &supersededBy.String
	}
	if reviewedAt.Valid {
		parsed, err := parseTime(reviewedAt.String)
		if err != nil {
			return knowledge.Item{}, err
		}
		value.ReviewedAt = &parsed
	}
	value.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return knowledge.Item{}, err
	}
	value.UpdatedAt, err = parseTime(updatedAt)
	return value, err
}

func queryCatalog(ctx context.Context, db *sql.DB, query string, args ...any) ([]knowledge.CatalogItem, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query knowledge catalog: %w", err)
	}
	defer rows.Close()
	result := []knowledge.CatalogItem{}
	for rows.Next() {
		var value knowledge.CatalogItem
		var tags, updatedAt string
		var reviewedAt, supersededBy sql.NullString
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.Revision, &value.Kind, &value.Status, &value.Title, &value.Summary, &value.Snippet,
			&tags, &value.Visibility, &value.Sensitivity, &value.ContentHash, &reviewedAt, &supersededBy, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan knowledge catalog: %w", err)
		}
		if err := json.Unmarshal([]byte(tags), &value.Tags); err != nil {
			return nil, fmt.Errorf("decode knowledge catalog tags: %w", err)
		}
		value.Snippet = knowledge.Snippet(value.Snippet)
		if supersededBy.Valid {
			value.SupersededBy = &supersededBy.String
		}
		if reviewedAt.Valid {
			parsed, err := parseTime(reviewedAt.String)
			if err != nil {
				return nil, err
			}
			value.ReviewedAt = &parsed
		}
		value.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func insertEvent(ctx context.Context, tx *sql.Tx, id, itemID, eventType, body string, revision int64, actor contextmodel.ActorSnapshot, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_events(id,item_id,type,body,item_revision,actor_id,actor_kind,actor_name,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, itemID, eventType, body, revision, actor.ID, actor.Kind, actor.Name, stamp(now))
	if err != nil {
		return fmt.Errorf("insert knowledge event: %w", mapSQLError(err))
	}
	return nil
}

func requireActiveProject(ctx context.Context, tx *sql.Tx, projectID string) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id=? AND deleted_at IS NULL", projectID).Scan(&count); err != nil {
		return fmt.Errorf("check knowledge project: %w", err)
	}
	if count != 1 {
		return knowledge.NewError(knowledge.CodeNotFound, "project was not found")
	}
	return nil
}

func appendFilters(query string, args []any, column string, values []string) (string, []any) {
	if len(values) == 0 {
		return query, args
	}
	query += " AND " + column + " IN (" + strings.TrimRight(strings.Repeat("?,", len(values)), ",") + ")"
	for _, value := range values {
		args = append(args, value)
	}
	return query, args
}

func kinds(values []knowledge.Kind) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = string(values[i])
	}
	return result
}
func statuses(values []knowledge.Status) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = string(values[i])
	}
	return result
}
func visibilities(values []contextmodel.Visibility) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = string(values[i])
	}
	return result
}
func sensitivities(values []contextmodel.Sensitivity) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = string(values[i])
	}
	return result
}

func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse knowledge timestamp: %w", err)
	}
	return parsed.UTC(), nil
}
func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return stamp(*value)
}

func mapSQLError(err error) error {
	var value sqlite3.Error
	if errors.As(err, &value) && value.Code == sqlite3.ErrConstraint {
		return knowledge.NewError(knowledge.CodeConflict, "knowledge item conflicts with existing data")
	}
	return err
}

-- +goose Up
ALTER TABLE context_records ADD COLUMN delivery TEXT NOT NULL DEFAULT 'ranked'
    CHECK (delivery IN ('always', 'ranked', 'manual'));
ALTER TABLE context_records ADD COLUMN review_after TEXT;
ALTER TABLE context_records ADD COLUMN expires_at TEXT;
ALTER TABLE context_records ADD COLUMN superseded_by TEXT REFERENCES context_records(id);

UPDATE context_records SET delivery='always' WHERE kind='instruction';

CREATE INDEX context_records_project_delivery
    ON context_records(project_id, delivery, deleted_at);
CREATE INDEX context_records_superseded_by
    ON context_records(superseded_by);

ALTER TABLE context_snapshot_items ADD COLUMN delivery TEXT NOT NULL DEFAULT 'ranked'
    CHECK (delivery IN ('always', 'ranked', 'manual'));
ALTER TABLE context_snapshot_items ADD COLUMN lane TEXT NOT NULL DEFAULT ''
    CHECK (lane IN ('', 'always', 'explicit', 'ranked'));
ALTER TABLE context_snapshot_items ADD COLUMN selection_score REAL NOT NULL DEFAULT 0;
ALTER TABLE context_snapshot_items ADD COLUMN matched_terms TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(matched_terms) AND json_type(matched_terms)='array');
ALTER TABLE context_snapshot_items ADD COLUMN reasons TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(reasons) AND json_type(reasons)='array');

CREATE TABLE context_snapshot_assembly_metadata (
    snapshot_id TEXT PRIMARY KEY REFERENCES context_snapshots(id) ON DELETE CASCADE,
    assembly_version TEXT NOT NULL CHECK (length(trim(assembly_version)) > 0),
    context_override_reason TEXT NOT NULL DEFAULT '',
    task_query_hash TEXT NOT NULL CHECK (length(task_query_hash) = 64),
    candidate_set_hash TEXT NOT NULL CHECK (length(candidate_set_hash) = 64),
    candidate_count INTEGER NOT NULL CHECK (candidate_count >= 0),
    selected_count INTEGER NOT NULL CHECK (selected_count >= 0),
    durable_budget_bytes INTEGER NOT NULL CHECK (durable_budget_bytes > 0),
    retrieval_budget_bytes INTEGER NOT NULL CHECK (retrieval_budget_bytes > 0),
    total_budget_bytes INTEGER NOT NULL CHECK (total_budget_bytes > 0),
    always_bytes INTEGER NOT NULL CHECK (always_bytes >= 0),
    explicit_bytes INTEGER NOT NULL CHECK (explicit_bytes >= 0),
    ranked_bytes INTEGER NOT NULL CHECK (ranked_bytes >= 0),
    durable_bytes INTEGER NOT NULL CHECK (durable_bytes >= 0),
    retrieval_bytes INTEGER NOT NULL CHECK (retrieval_bytes >= 0),
    total_bytes INTEGER NOT NULL CHECK (total_bytes >= 0),
    warnings TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(warnings) AND json_type(warnings)='array')
);

-- +goose Down
DROP TABLE context_snapshot_assembly_metadata;

ALTER TABLE context_snapshot_items DROP COLUMN reasons;
ALTER TABLE context_snapshot_items DROP COLUMN matched_terms;
ALTER TABLE context_snapshot_items DROP COLUMN selection_score;
ALTER TABLE context_snapshot_items DROP COLUMN lane;
ALTER TABLE context_snapshot_items DROP COLUMN delivery;

DROP INDEX context_records_superseded_by;
DROP INDEX context_records_project_delivery;

ALTER TABLE context_records DROP COLUMN superseded_by;
ALTER TABLE context_records DROP COLUMN expires_at;
ALTER TABLE context_records DROP COLUMN review_after;
ALTER TABLE context_records DROP COLUMN delivery;

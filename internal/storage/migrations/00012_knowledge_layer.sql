-- +goose Up
CREATE TABLE knowledge_items (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    kind TEXT NOT NULL CHECK (kind IN ('architecture', 'domain', 'decision', 'runbook', 'note')),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'current', 'superseded')),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    summary TEXT NOT NULL CHECK (length(trim(summary)) > 0),
    body TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags) AND json_type(tags)='array'),
    visibility TEXT NOT NULL CHECK (visibility IN ('shared', 'local_only')),
    sensitivity TEXT NOT NULL CHECK (sensitivity IN ('normal', 'private')),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    provenance TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(provenance) AND json_type(provenance)='array'),
    reviewed_at TEXT,
    superseded_by TEXT REFERENCES knowledge_items(id),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX knowledge_items_project_status_updated
    ON knowledge_items(project_id, status, updated_at DESC, id);
CREATE INDEX knowledge_items_superseded_by
    ON knowledge_items(superseded_by);

CREATE TABLE knowledge_events (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (length(trim(type)) > 0),
    body TEXT NOT NULL DEFAULT '',
    item_revision INTEGER NOT NULL CHECK (item_revision >= 1),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL
);

CREATE INDEX knowledge_events_item_created_id
    ON knowledge_events(item_id, created_at, id);

ALTER TABLE context_snapshots ADD COLUMN knowledge_catalog TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(knowledge_catalog) AND json_type(knowledge_catalog)='array');

ALTER TABLE context_snapshot_assembly_metadata ADD COLUMN knowledge_candidate_count INTEGER NOT NULL DEFAULT 0 CHECK (knowledge_candidate_count >= 0);
ALTER TABLE context_snapshot_assembly_metadata ADD COLUMN knowledge_selected_count INTEGER NOT NULL DEFAULT 0 CHECK (knowledge_selected_count >= 0);
ALTER TABLE context_snapshot_assembly_metadata ADD COLUMN knowledge_budget_bytes INTEGER NOT NULL DEFAULT 4096 CHECK (knowledge_budget_bytes > 0);
ALTER TABLE context_snapshot_assembly_metadata ADD COLUMN knowledge_used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (knowledge_used_bytes >= 0);
ALTER TABLE context_snapshot_assembly_metadata ADD COLUMN knowledge_truncated INTEGER NOT NULL DEFAULT 0 CHECK (knowledge_truncated IN (0,1));

-- +goose Down
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN knowledge_truncated;
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN knowledge_used_bytes;
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN knowledge_budget_bytes;
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN knowledge_selected_count;
ALTER TABLE context_snapshot_assembly_metadata DROP COLUMN knowledge_candidate_count;
ALTER TABLE context_snapshots DROP COLUMN knowledge_catalog;
DROP TABLE knowledge_events;
DROP TABLE knowledge_items;

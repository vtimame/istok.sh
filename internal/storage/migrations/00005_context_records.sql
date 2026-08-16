-- +goose Up
CREATE TABLE context_records (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    kind TEXT NOT NULL CHECK (kind IN ('note', 'decision', 'instruction', 'constraint')),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    body TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    source TEXT NOT NULL CHECK (source IN ('user', 'agent', 'import')),
    visibility TEXT NOT NULL CHECK (visibility IN ('shared', 'local_only')),
    sensitivity TEXT NOT NULL CHECK (sensitivity IN ('normal', 'private')),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX context_records_project_id ON context_records(project_id);
CREATE INDEX context_records_project_id_deleted ON context_records(project_id, deleted_at);

CREATE TABLE context_events (
    id TEXT PRIMARY KEY,
    record_id TEXT NOT NULL REFERENCES context_records(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (length(trim(type)) > 0),
    body TEXT NOT NULL DEFAULT '',
    record_revision INTEGER NOT NULL CHECK (record_revision >= 1),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL
);

CREATE INDEX context_events_record_created_id ON context_events(record_id, created_at, id);

-- +goose Down
DROP TABLE context_events;
DROP TABLE context_records;

-- +goose Up
CREATE TABLE hub_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT,
    revoked_at TEXT
);

CREATE TABLE enrollments (
    secret_hash TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    device_id TEXT REFERENCES devices(id)
);

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    meta BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE rows (
    project_id TEXT NOT NULL REFERENCES projects(id),
    table_name TEXT NOT NULL,
    row_key TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('mutable', 'append', 'group')),
    revision INTEGER NOT NULL,
    tag TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    body BLOB NOT NULL,
    seq INTEGER NOT NULL UNIQUE,
    device_id TEXT NOT NULL REFERENCES devices(id),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, table_name, row_key)
);

CREATE INDEX rows_project_seq ON rows(project_id, seq);

-- +goose Down
DROP TABLE rows;
DROP TABLE projects;
DROP TABLE enrollments;
DROP TABLE devices;
DROP TABLE hub_meta;

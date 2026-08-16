-- +goose Up
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE TABLE project_roots (
    id INTEGER PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    canonical_path TEXT NOT NULL,
    path_key TEXT NOT NULL,
    active_at TEXT NOT NULL,
    detached_at TEXT
);

CREATE UNIQUE INDEX project_roots_active_path_key
    ON project_roots(path_key)
    WHERE detached_at IS NULL;

CREATE UNIQUE INDEX project_roots_one_active_per_project
    ON project_roots(project_id)
    WHERE detached_at IS NULL;

CREATE INDEX project_roots_project_history ON project_roots(project_id, id DESC);

-- +goose Down
DROP TABLE project_roots;
DROP TABLE projects;

-- +goose Up
-- Device-local state of hub synchronization. Export bundles list their tables
-- explicitly, so these tables stay on the device.

-- Projects this device syncs, with the hub cursor reached so far.
CREATE TABLE sync_projects (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    cursor INTEGER NOT NULL DEFAULT 0 CHECK (cursor >= 0),
    enabled_at TEXT NOT NULL,
    synced_at TEXT
);

-- What the hub holds for each row (revision, tag) and the tag of the local row
-- at that moment. A row is pushed when it changed locally since then.
CREATE TABLE sync_rows (
    project_id TEXT NOT NULL REFERENCES sync_projects(project_id) ON DELETE CASCADE,
    table_name TEXT NOT NULL,
    row_key TEXT NOT NULL,
    revision INTEGER NOT NULL,
    hub_tag TEXT NOT NULL,
    local_tag TEXT NOT NULL,
    PRIMARY KEY (project_id, table_name, row_key)
);

-- +goose Down
DROP TABLE sync_rows;
DROP TABLE sync_projects;

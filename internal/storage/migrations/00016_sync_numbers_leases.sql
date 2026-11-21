-- +goose Up
-- Task numbers the hub confirmed. A task of a synced project without a row,
-- or with another number, still has to claim its number on the hub.
CREATE TABLE sync_task_numbers (
    project_id TEXT NOT NULL REFERENCES sync_projects(project_id) ON DELETE CASCADE,
    task_id TEXT NOT NULL,
    number INTEGER NOT NULL,
    PRIMARY KEY (project_id, task_id)
);

-- Runs claimed on this device, whose task lease it keeps on the hub until
-- the run ends.
CREATE TABLE sync_leases (
    project_id TEXT NOT NULL REFERENCES sync_projects(project_id) ON DELETE CASCADE,
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    PRIMARY KEY (project_id, task_id)
);

-- +goose Down
DROP TABLE sync_leases;
DROP TABLE sync_task_numbers;

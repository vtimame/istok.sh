-- +goose Up
-- Task numbers are handed out by the hub, so devices never pick the same one.
-- The hub knows which task holds a number, not what the task is.
CREATE TABLE task_numbers (
    project_id TEXT NOT NULL REFERENCES projects(id),
    number INTEGER NOT NULL CHECK (number > 0),
    task_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (project_id, number),
    UNIQUE (project_id, task_id)
);

-- One device works on a task at a time: the lease of its run.
CREATE TABLE leases (
    project_id TEXT NOT NULL REFERENCES projects(id),
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    device_id TEXT NOT NULL REFERENCES devices(id),
    expires_at TEXT NOT NULL,
    PRIMARY KEY (project_id, task_id)
);

-- +goose Down
DROP TABLE leases;
DROP TABLE task_numbers;

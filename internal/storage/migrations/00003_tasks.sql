-- +goose Up
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK (number > 0),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'blocked', 'done')),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    UNIQUE(project_id, number)
);

CREATE INDEX tasks_project_status ON tasks(project_id, status);

CREATE TABLE project_task_counters (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    next_number INTEGER NOT NULL CHECK (next_number > 0)
);

INSERT INTO project_task_counters(project_id, next_number)
    SELECT id, 1 FROM projects;

CREATE TABLE task_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (length(trim(type)) > 0),
    body TEXT NOT NULL DEFAULT '',
    task_revision INTEGER NOT NULL CHECK (task_revision >= 1),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL
);

CREATE INDEX task_events_task_created_id ON task_events(task_id, created_at, id);

-- +goose Down
DROP TABLE task_events;
DROP TABLE project_task_counters;
DROP TABLE tasks;

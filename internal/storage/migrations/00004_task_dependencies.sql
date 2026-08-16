-- +goose Up
CREATE UNIQUE INDEX tasks_id_project_id ON tasks(id, project_id);

CREATE TABLE task_dependencies (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    blocker_task_id TEXT NOT NULL,
    blocked_task_id TEXT NOT NULL,
    edge_type TEXT NOT NULL DEFAULT 'blocks' CHECK (edge_type = 'blocks'),
    created_at TEXT NOT NULL,
    CHECK (blocker_task_id <> blocked_task_id),
    UNIQUE(project_id, blocker_task_id, blocked_task_id, edge_type),
    FOREIGN KEY (blocker_task_id, project_id) REFERENCES tasks(id, project_id) ON DELETE CASCADE,
    FOREIGN KEY (blocked_task_id, project_id) REFERENCES tasks(id, project_id) ON DELETE CASCADE
);
CREATE INDEX task_dependencies_blocked ON task_dependencies(project_id, blocked_task_id);
CREATE INDEX task_dependencies_blocker ON task_dependencies(project_id, blocker_task_id);

-- +goose Down
DROP TABLE task_dependencies;
DROP INDEX tasks_id_project_id;

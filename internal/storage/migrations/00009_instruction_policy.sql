-- +goose Up
ALTER TABLE context_records ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1));
ALTER TABLE context_records ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('critical', 'high', 'normal', 'low'));
ALTER TABLE context_records ADD COLUMN scope TEXT NOT NULL DEFAULT 'project' CHECK (scope = 'project');

ALTER TABLE context_snapshot_items ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1));
ALTER TABLE context_snapshot_items ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('critical', 'high', 'normal', 'low'));
ALTER TABLE context_snapshot_items ADD COLUMN scope TEXT NOT NULL DEFAULT 'project' CHECK (scope = 'project');

CREATE INDEX context_records_project_instruction_enabled ON context_records(project_id, kind, enabled);

-- +goose Down
DROP INDEX context_records_project_instruction_enabled;

ALTER TABLE context_snapshot_items DROP COLUMN scope;
ALTER TABLE context_snapshot_items DROP COLUMN priority;
ALTER TABLE context_snapshot_items DROP COLUMN enabled;

ALTER TABLE context_records DROP COLUMN scope;
ALTER TABLE context_records DROP COLUMN priority;
ALTER TABLE context_records DROP COLUMN enabled;

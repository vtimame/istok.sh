-- +goose Up
CREATE TABLE run_leases (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    lease_id TEXT NOT NULL UNIQUE CHECK (length(trim(lease_id)) > 0),
    owner_id TEXT NOT NULL CHECK (length(trim(owner_id)) > 0),
    owner_kind TEXT NOT NULL CHECK (length(trim(owner_kind)) > 0),
    owner_name TEXT NOT NULL CHECK (length(trim(owner_name)) > 0),
    heartbeat_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO run_leases (run_id, lease_id, owner_id, owner_kind, owner_name, heartbeat_at, expires_at, updated_at)
SELECT id, id, actor_id, actor_kind, actor_name, updated_at, updated_at, updated_at FROM runs;

ALTER TABLE executions ADD COLUMN dangerous_override TEXT NOT NULL DEFAULT '';

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('stdout', 'stderr')),
    relative_path TEXT NOT NULL CHECK (length(trim(relative_path)) > 0),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
    original_size INTEGER NOT NULL CHECK (original_size >= 0),
    stored_size INTEGER NOT NULL CHECK (stored_size >= 0 AND stored_size <= original_size),
    truncated INTEGER NOT NULL CHECK (truncated IN (0, 1)),
    media_type TEXT NOT NULL CHECK (length(trim(media_type)) > 0),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL,
    UNIQUE(execution_id, kind)
);
CREATE INDEX artifacts_run_created ON artifacts(run_id, created_at, id);
CREATE INDEX artifacts_execution_created ON artifacts(execution_id, created_at, id);
CREATE UNIQUE INDEX executions_one_running_per_run ON executions(run_id) WHERE status = 'running';

-- +goose Down
DROP INDEX executions_one_running_per_run;
DROP INDEX artifacts_execution_created;
DROP INDEX artifacts_run_created;
DROP TABLE artifacts;
ALTER TABLE executions DROP COLUMN dangerous_override;
DROP TABLE run_leases;

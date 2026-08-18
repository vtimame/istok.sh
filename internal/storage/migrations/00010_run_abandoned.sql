-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys=OFF;
BEGIN;
CREATE TABLE runs_new (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    context_snapshot_id TEXT NOT NULL UNIQUE REFERENCES context_snapshots(id),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    status TEXT NOT NULL CHECK (status IN ('active', 'succeeded', 'failed', 'blocked', 'cancelled', 'abandoned')),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0), actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0), actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    base_branch TEXT NOT NULL DEFAULT '', base_commit TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    finished_at TEXT, finished_actor_id TEXT, finished_actor_kind TEXT, finished_actor_name TEXT,
    result_summary TEXT NOT NULL DEFAULT '', validation_override TEXT NOT NULL DEFAULT '',
    CHECK ((status = 'active' AND finished_at IS NULL) OR (status != 'active' AND finished_at IS NOT NULL)),
    CHECK ((status = 'active' AND finished_actor_id IS NULL AND finished_actor_kind IS NULL AND finished_actor_name IS NULL) OR (status != 'active' AND length(trim(finished_actor_id)) > 0 AND length(trim(finished_actor_kind)) > 0 AND length(trim(finished_actor_name)) > 0))
);
INSERT INTO runs_new SELECT * FROM runs;
DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;
CREATE UNIQUE INDEX runs_one_active_per_task ON runs(task_id) WHERE status = 'active';
CREATE INDEX runs_task_started ON runs(task_id, started_at DESC, id DESC);
PRAGMA foreign_key_check;
COMMIT;
PRAGMA foreign_keys=ON;

-- +goose Down
PRAGMA foreign_keys=OFF;
BEGIN;
CREATE TABLE runs_new (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    context_snapshot_id TEXT NOT NULL UNIQUE REFERENCES context_snapshots(id),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    status TEXT NOT NULL CHECK (status IN ('active', 'succeeded', 'failed', 'blocked', 'cancelled')),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0), actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0), actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    base_branch TEXT NOT NULL DEFAULT '', base_commit TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    finished_at TEXT, finished_actor_id TEXT, finished_actor_kind TEXT, finished_actor_name TEXT,
    result_summary TEXT NOT NULL DEFAULT '', validation_override TEXT NOT NULL DEFAULT '',
    CHECK ((status = 'active' AND finished_at IS NULL) OR (status != 'active' AND finished_at IS NOT NULL)),
    CHECK ((status = 'active' AND finished_actor_id IS NULL AND finished_actor_kind IS NULL AND finished_actor_name IS NULL) OR (status != 'active' AND length(trim(finished_actor_id)) > 0 AND length(trim(finished_actor_kind)) > 0 AND length(trim(finished_actor_name)) > 0))
);
INSERT INTO runs_new SELECT id,task_id,context_snapshot_id,revision,CASE WHEN status='abandoned' THEN 'cancelled' ELSE status END,actor_id,actor_kind,actor_name,base_branch,base_commit,started_at,updated_at,finished_at,finished_actor_id,finished_actor_kind,finished_actor_name,result_summary,validation_override FROM runs;
DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;
CREATE UNIQUE INDEX runs_one_active_per_task ON runs(task_id) WHERE status = 'active';
CREATE INDEX runs_task_started ON runs(task_id, started_at DESC, id DESC);
PRAGMA foreign_key_check;
COMMIT;
PRAGMA foreign_keys=ON;

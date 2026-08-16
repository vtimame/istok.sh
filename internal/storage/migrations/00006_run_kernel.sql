-- +goose Up
CREATE TABLE context_snapshots (
    id TEXT PRIMARY KEY,
    schema_version TEXT NOT NULL CHECK (length(trim(schema_version)) > 0),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    generated_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE context_snapshot_items (
    snapshot_id TEXT NOT NULL REFERENCES context_snapshots(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    record_id TEXT NOT NULL,
    record_revision INTEGER NOT NULL CHECK (record_revision >= 1),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    kind TEXT NOT NULL CHECK (kind IN ('note', 'decision', 'instruction', 'constraint')),
    source TEXT NOT NULL CHECK (source IN ('user', 'agent', 'import')),
    visibility TEXT NOT NULL CHECK (visibility IN ('shared', 'local_only')),
    sensitivity TEXT NOT NULL CHECK (sensitivity IN ('normal', 'private')),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    body TEXT NOT NULL DEFAULT '',
    snippet TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    PRIMARY KEY(snapshot_id, position),
    UNIQUE(snapshot_id, record_id)
);

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    context_snapshot_id TEXT NOT NULL UNIQUE REFERENCES context_snapshots(id),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    status TEXT NOT NULL CHECK (status IN ('active', 'succeeded', 'failed', 'blocked', 'cancelled')),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    base_branch TEXT NOT NULL DEFAULT '',
    base_commit TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    finished_actor_id TEXT,
    finished_actor_kind TEXT,
    finished_actor_name TEXT,
    result_summary TEXT NOT NULL DEFAULT '',
    validation_override TEXT NOT NULL DEFAULT '',
    CHECK (
        (status = 'active' AND finished_at IS NULL)
        OR (status != 'active' AND finished_at IS NOT NULL)
    ),
    CHECK (
        (
            status = 'active'
            AND finished_actor_id IS NULL
            AND finished_actor_kind IS NULL
            AND finished_actor_name IS NULL
        )
        OR (
            status != 'active'
            AND length(trim(finished_actor_id)) > 0
            AND length(trim(finished_actor_kind)) > 0
            AND length(trim(finished_actor_name)) > 0
        )
    )
);

CREATE UNIQUE INDEX runs_one_active_per_task
    ON runs(task_id)
    WHERE status = 'active';
CREATE INDEX runs_task_started ON runs(task_id, started_at DESC, id DESC);

CREATE TABLE executions (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    argv TEXT NOT NULL CHECK (json_valid(argv) AND json_type(argv) = 'array'),
    cwd TEXT NOT NULL CHECK (length(trim(cwd)) > 0),
    exit_code INTEGER,
    duration_ms INTEGER CHECK (duration_ms IS NULL OR duration_ms >= 0),
    signal TEXT NOT NULL DEFAULT '',
    timed_out INTEGER NOT NULL DEFAULT 0 CHECK (timed_out IN (0, 1)),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    CHECK (
        (status = 'running' AND finished_at IS NULL)
        OR (status != 'running' AND finished_at IS NOT NULL)
    )
);

CREATE INDEX executions_run_started ON executions(run_id, started_at, id);

CREATE TABLE validations (
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('executed', 'attested')),
    status TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
    command TEXT NOT NULL CHECK (length(trim(command)) > 0),
    exit_code INTEGER,
    duration_ms INTEGER CHECK (duration_ms IS NULL OR duration_ms >= 0),
    summary TEXT NOT NULL CHECK (length(trim(summary)) > 0),
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL
);

CREATE INDEX validations_execution_created ON validations(execution_id, created_at, id);

CREATE TABLE task_completions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
    run_id TEXT REFERENCES runs(id),
    validation_id TEXT REFERENCES validations(id),
    note TEXT NOT NULL CHECK (length(trim(note)) > 0),
    override_reason TEXT NOT NULL DEFAULT '',
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    actor_kind TEXT NOT NULL CHECK (length(trim(actor_kind)) > 0),
    actor_name TEXT NOT NULL CHECK (length(trim(actor_name)) > 0),
    created_at TEXT NOT NULL,
    CHECK (
        (run_id IS NOT NULL AND validation_id IS NOT NULL)
        OR length(trim(override_reason)) > 0
    )
);

-- +goose Down
DROP TABLE task_completions;
DROP TABLE validations;
DROP TABLE executions;
DROP INDEX runs_task_started;
DROP INDEX runs_one_active_per_task;
DROP TABLE runs;
DROP TABLE context_snapshot_items;
DROP TABLE context_snapshots;

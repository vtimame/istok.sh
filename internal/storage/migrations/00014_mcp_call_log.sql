-- +goose Up
-- Device-local log of MCP tool calls. It records which tools agents call and
-- how the calls end, never their arguments or results. Export bundles list
-- their tables explicitly, so this table stays on the device.
CREATE TABLE mcp_calls (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL CHECK (length(trim(session_id)) > 0),
    client_name TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
    project_id TEXT,
    tool TEXT NOT NULL CHECK (length(trim(tool)) > 0),
    started_at TEXT NOT NULL,
    duration_ms INTEGER NOT NULL CHECK (duration_ms >= 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('ok', 'error')),
    error_code TEXT NOT NULL DEFAULT '',
    CHECK ((outcome = 'ok' AND error_code = '') OR (outcome = 'error' AND length(trim(error_code)) > 0))
);
CREATE INDEX mcp_calls_started ON mcp_calls(started_at);
CREATE INDEX mcp_calls_session ON mcp_calls(session_id, started_at);
CREATE INDEX mcp_calls_project_started ON mcp_calls(project_id, started_at);

-- +goose Down
DROP INDEX mcp_calls_project_started;
DROP INDEX mcp_calls_session;
DROP INDEX mcp_calls_started;
DROP TABLE mcp_calls;

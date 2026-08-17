-- +goose Up
CREATE TABLE context_snapshot_retrieval_metadata (
    snapshot_id TEXT PRIMARY KEY
        REFERENCES context_snapshots(id) ON DELETE CASCADE,
    without_retrieval INTEGER NOT NULL DEFAULT 0
        CHECK (without_retrieval IN (0, 1)),
    override_reason TEXT NOT NULL DEFAULT '',
    CHECK (
        (without_retrieval = 0 AND length(trim(override_reason)) = 0)
        OR (without_retrieval = 1 AND length(trim(override_reason)) > 0)
    )
);

CREATE TABLE context_snapshot_retrieval_items (
    snapshot_id TEXT NOT NULL REFERENCES context_snapshots(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    item_id TEXT NOT NULL,
    contract_version TEXT NOT NULL CHECK (length(trim(contract_version)) > 0),
    chunk_id TEXT NOT NULL CHECK (length(trim(chunk_id)) > 0),
    kind TEXT NOT NULL CHECK (kind IN ('file_chunk', 'symbol')),
    path TEXT NOT NULL CHECK (length(trim(path)) > 0),
    language TEXT NOT NULL CHECK (length(trim(language)) > 0),
    symbol TEXT NOT NULL DEFAULT '',
    line_start INTEGER NOT NULL CHECK (line_start >= 1),
    line_end INTEGER NOT NULL CHECK (line_end >= line_start),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    snippet TEXT NOT NULL CHECK (length(trim(snippet)) > 0),
    score REAL NOT NULL, lexical_score REAL NOT NULL, graph_score REAL NOT NULL,
    matched_terms TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(matched_terms) AND json_type(matched_terms) = 'array'),
    provenance TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(provenance) AND json_type(provenance) = 'array'),
    reasons TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(reasons) AND json_type(reasons) = 'array'),
    visibility TEXT NOT NULL CHECK (visibility = 'local_only'),
    PRIMARY KEY (snapshot_id, position),
    UNIQUE (snapshot_id, item_id)
);

-- +goose Down
DROP TABLE context_snapshot_retrieval_items;
DROP TABLE context_snapshot_retrieval_metadata;

package graphstore

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"s26.dev/istok-cli/internal/retrieval"
)

const (
	sqlCreateGraphNodes = `
		CREATE TABLE IF NOT EXISTS graph_nodes (
			id TEXT PRIMARY KEY NOT NULL,
			kind TEXT NOT NULL,
			language TEXT NOT NULL,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			qualified_name TEXT NOT NULL,
			signature TEXT NOT NULL,
			line_start INTEGER NOT NULL,
			line_end INTEGER NOT NULL,
			content_hash TEXT NOT NULL
		)
	`
	sqlCreateGraphEdges = `
		CREATE TABLE IF NOT EXISTS graph_edges (
			source_id TEXT NOT NULL,
			target_id TEXT,
			target_name TEXT NOT NULL,
			kind TEXT NOT NULL,
			provenance TEXT NOT NULL,
			confidence REAL NOT NULL,
			evidence_path TEXT NOT NULL,
			evidence_line INTEGER NOT NULL,
			FOREIGN KEY(source_id) REFERENCES graph_nodes(id) ON DELETE CASCADE,
			FOREIGN KEY(target_id) REFERENCES graph_nodes(id) ON DELETE SET NULL
		)
	`
	sqlCreateGraphDiagnostics = `
		CREATE TABLE IF NOT EXISTS graph_diagnostics (
			path TEXT NOT NULL,
			language TEXT NOT NULL,
			line INTEGER NOT NULL,
			message TEXT NOT NULL
		)
	`
)

const (
	sqlIndexNodesPath          = `CREATE INDEX IF NOT EXISTS idx_graph_nodes_path ON graph_nodes(path)`
	sqlIndexNodesQualifiedName = `CREATE INDEX IF NOT EXISTS idx_graph_nodes_qualified_name ON graph_nodes(qualified_name)`
	sqlIndexNodesName          = `CREATE INDEX IF NOT EXISTS idx_graph_nodes_name ON graph_nodes(name)`
	sqlIndexEdgesSource        = `CREATE INDEX IF NOT EXISTS idx_graph_edges_source_id ON graph_edges(source_id)`
	sqlIndexEdgesTargetID      = `CREATE INDEX IF NOT EXISTS idx_graph_edges_target_id ON graph_edges(target_id)`
	sqlIndexEdgesTargetName    = `CREATE INDEX IF NOT EXISTS idx_graph_edges_target_name ON graph_edges(target_name)`
	sqlIndexEdgesKind          = `CREATE INDEX IF NOT EXISTS idx_graph_edges_kind ON graph_edges(kind)`
	sqlIndexDiagPath           = `CREATE INDEX IF NOT EXISTS idx_graph_diagnostics_path ON graph_diagnostics(path)`
)

func Initialize(db *sql.DB) error {
	if db == nil {
		return errRepositoryNil
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("initialize graph database: enable foreign keys: %w", err)
	}

	statements := []string{
		sqlCreateGraphNodes,
		sqlCreateGraphEdges,
		sqlCreateGraphDiagnostics,
		sqlIndexNodesPath,
		sqlIndexNodesQualifiedName,
		sqlIndexNodesName,
		sqlIndexEdgesSource,
		sqlIndexEdgesTargetID,
		sqlIndexEdgesTargetName,
		sqlIndexEdgesKind,
		sqlIndexDiagPath,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}

	return nil
}

func normalizePath(path string) string {
	return retrieval.NormalizePath(path)
}

func normalizeAndSortPaths(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizePath(value)
		if normalized != "" {
			seen[normalized] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)

	return result
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}

	values := make([]string, count)
	for i := 0; i < count; i++ {
		values[i] = "?"
	}
	return strings.Join(values, ",")
}

func toArgs(values []string) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func normalizeAndLimit(value int, fallback, hardLimit int) int {
	if value <= 0 {
		return fallback
	}
	if value > hardLimit {
		return hardLimit
	}
	return value
}

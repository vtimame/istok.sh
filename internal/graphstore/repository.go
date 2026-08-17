package graphstore

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"s26.dev/istok-cli/internal/codegraph"
)

func (r *Repository) Replace(
	ctx context.Context,
	tx *sql.Tx,
	replacePaths []string,
	files []codegraph.FileGraph,
) error {
	if r == nil || r.db == nil {
		return errRepositoryNil
	}
	if tx == nil {
		return errTransactionNil
	}

	normalizedPaths := normalizeAndSortPaths(replacePaths)
	normalizedFiles, err := normalizeAndSortFileGraphs(files)
	if err != nil {
		return err
	}
	for _, file := range normalizedFiles {
		normalizedPaths = append(normalizedPaths, file.Path)
	}
	normalizedPaths = uniqueOrdered(normalizedPaths)

	if len(normalizedPaths) > 0 {
		args := toArgs(normalizedPaths)
		query := fmt.Sprintf("DELETE FROM graph_nodes WHERE path IN (%s)", placeholders(len(normalizedPaths)))
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("replace: delete old nodes: %w", err)
		}
		query = fmt.Sprintf("DELETE FROM graph_diagnostics WHERE path IN (%s)", placeholders(len(normalizedPaths)))
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("replace: delete old diagnostics: %w", err)
		}
	}

	insertNode, err := tx.PrepareContext(
		ctx,
		`INSERT INTO graph_nodes(id, kind, language, path, name, qualified_name, signature, line_start, line_end, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("replace: prepare nodes: %w", err)
	}
	defer insertNode.Close()

	insertEdge, err := tx.PrepareContext(
		ctx,
		`INSERT INTO graph_edges(source_id, target_id, target_name, kind, provenance, confidence, evidence_path, evidence_line)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("replace: prepare edges: %w", err)
	}
	defer insertEdge.Close()

	insertDiagnostic, err := tx.PrepareContext(
		ctx,
		`INSERT INTO graph_diagnostics(path, language, line, message)
		VALUES (?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("replace: prepare diagnostics: %w", err)
	}
	defer insertDiagnostic.Close()

	nodesByID := make(map[string]struct{}, len(normalizedFiles)*4)
	for _, file := range normalizedFiles {
		fileNodeIDs := make(map[string]struct{}, len(file.Nodes))
		for _, node := range file.Nodes {
			if _, ok := nodesByID[node.ID]; ok {
				return fmt.Errorf("replace: duplicate node id %q", node.ID)
			}
			nodesByID[node.ID] = struct{}{}
			fileNodeIDs[node.ID] = struct{}{}

			if _, err := insertNode.ExecContext(
				ctx,
				node.ID,
				node.Kind,
				node.Language,
				node.Path,
				node.Name,
				node.QualifiedName,
				node.Signature,
				node.LineStart,
				node.LineEnd,
				node.ContentHash,
			); err != nil {
				return fmt.Errorf("replace: insert node %q: %w", node.ID, err)
			}
		}

		for _, edge := range file.Edges {
			sourceID := strings.TrimSpace(edge.SourceID)
			if sourceID == "" {
				return fmt.Errorf("replace: edge with missing source id in %s", file.Path)
			}
			if _, ok := fileNodeIDs[sourceID]; !ok {
				return fmt.Errorf("replace: source %q is not owned by %q", sourceID, file.Path)
			}
			var targetID any
			if edge.TargetID != "" {
				if _, ok := fileNodeIDs[edge.TargetID]; !ok {
					return fmt.Errorf("replace: extracted target %q is not owned by %q", edge.TargetID, file.Path)
				}
				targetID = edge.TargetID
			}

			targetName := strings.TrimSpace(edge.TargetName)
			evidencePath := normalizePath(edge.EvidencePath)
			if evidencePath == "" {
				evidencePath = file.Path
			}
			confidence := edge.Confidence
			if confidence < 0 {
				confidence = 0
			}

			if _, err := insertEdge.ExecContext(
				ctx,
				sourceID,
				targetID,
				targetName,
				strings.TrimSpace(string(edge.Kind)),
				provenanceExtracted,
				confidence,
				evidencePath,
				edge.EvidenceLine,
			); err != nil {
				return fmt.Errorf("replace: insert edge from %q to %q: %w", sourceID, targetName, err)
			}
		}

		for _, diagnostic := range file.Diagnostics {
			if strings.TrimSpace(diagnostic.Message) == "" {
				continue
			}
			diagnosticPath := normalizePath(diagnostic.Path)
			if diagnosticPath == "" {
				diagnosticPath = file.Path
			}
			language := strings.TrimSpace(diagnostic.Language)
			if language == "" {
				language = file.Language
			}

			if _, err := insertDiagnostic.ExecContext(
				ctx,
				diagnosticPath,
				language,
				diagnostic.Line,
				diagnostic.Message,
			); err != nil {
				return fmt.Errorf("replace: insert diagnostic in %q: %w", diagnosticPath, err)
			}
		}
	}

	return r.resolveAllEdges(ctx, tx)
}

func (r *Repository) ListDiagnostics(ctx context.Context) ([]codegraph.Diagnostic, error) {
	if r == nil || r.db == nil {
		return nil, errRepositoryNil
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT path, language, line, message
		FROM graph_diagnostics
		ORDER BY path ASC, language ASC, line ASC, message ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	diagnostics := make([]codegraph.Diagnostic, 0)
	for rows.Next() {
		var diagnostic codegraph.Diagnostic
		if err := rows.Scan(&diagnostic.Path, &diagnostic.Language, &diagnostic.Line, &diagnostic.Message); err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return diagnostics, nil
}

func (r *Repository) LookupSymbols(
	ctx context.Context,
	request LookupSymbolsRequest,
) ([]codegraph.Node, error) {
	if r == nil || r.db == nil {
		return nil, errRepositoryNil
	}

	qualifiedName := strings.TrimSpace(request.QualifiedName)
	name := strings.TrimSpace(request.Name)
	if qualifiedName == "" && name == "" {
		return nil, errLookupRequestMissing()
	}
	limit := normalizeAndLimit(request.Limit, defaultLookupLimit, maxResultLimit)

	var query string
	var args []any
	if qualifiedName != "" && name != "" {
		query = `
			SELECT id, kind, language, path, name, qualified_name, signature, line_start, line_end, content_hash
			FROM graph_nodes
			WHERE qualified_name = ? OR name = ?
			ORDER BY CASE WHEN qualified_name = ? THEN 0 ELSE 1 END, path ASC, line_start ASC, line_end ASC, id ASC
			LIMIT ?
		`
		args = append(args, qualifiedName, name, qualifiedName, limit)
	} else if qualifiedName != "" {
		query = `
			SELECT id, kind, language, path, name, qualified_name, signature, line_start, line_end, content_hash
			FROM graph_nodes
			WHERE qualified_name = ?
			ORDER BY path ASC, line_start ASC, line_end ASC, id ASC
			LIMIT ?
		`
		args = append(args, qualifiedName, limit)
	} else {
		query = `
			SELECT id, kind, language, path, name, qualified_name, signature, line_start, line_end, content_hash
			FROM graph_nodes
			WHERE name = ?
			ORDER BY path ASC, line_start ASC, line_end ASC, id ASC
			LIMIT ?
		`
		args = append(args, name, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := make([]codegraph.Node, 0, limit)
	for rows.Next() {
		var node codegraph.Node
		if err := rows.Scan(
			&node.ID,
			&node.Kind,
			&node.Language,
			&node.Path,
			&node.Name,
			&node.QualifiedName,
			&node.Signature,
			&node.LineStart,
			&node.LineEnd,
			&node.ContentHash,
		); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return nodes, nil
}

func (r *Repository) Neighbors(
	ctx context.Context,
	request NeighborsRequest,
) ([]codegraph.Node, error) {
	if r == nil || r.db == nil {
		return nil, errRepositoryNil
	}

	sourceID := strings.TrimSpace(request.SourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("neighbors: source id is required")
	}
	limit := normalizeAndLimit(request.Limit, defaultLookupLimit, maxResultLimit)

	query := `
		SELECT n.id, n.kind, n.language, n.path, n.name, n.qualified_name, n.signature, n.line_start, n.line_end, n.content_hash
		FROM graph_edges e
		JOIN graph_nodes n ON n.id = e.target_id
		WHERE e.source_id = ? AND e.target_id IS NOT NULL
	`
	args := []any{sourceID}
	kinds := make([]string, 0, len(request.Kinds))
	for _, value := range request.Kinds {
		kind := strings.TrimSpace(string(value))
		if kind == "" {
			continue
		}
		kinds = append(kinds, kind)
	}
	if len(kinds) > 0 {
		query += " AND e.kind IN (" + placeholders(len(kinds)) + ")"
		sort.Strings(kinds)
		for _, value := range kinds {
			args = append(args, value)
		}
	}
	query += ` ORDER BY n.path ASC, n.line_start ASC, n.id ASC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	neighbors := make([]codegraph.Node, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var node codegraph.Node
		if err := rows.Scan(
			&node.ID,
			&node.Kind,
			&node.Language,
			&node.Path,
			&node.Name,
			&node.QualifiedName,
			&node.Signature,
			&node.LineStart,
			&node.LineEnd,
			&node.ContentHash,
		); err != nil {
			return nil, err
		}
		if _, ok := seen[node.ID]; ok {
			continue
		}
		seen[node.ID] = struct{}{}
		neighbors = append(neighbors, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return neighbors, nil
}

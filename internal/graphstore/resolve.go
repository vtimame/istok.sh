package graphstore

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/vtimame/istok.sh/internal/codegraph"
)

const (
	confidenceExtracted = 1.0
	confidenceResolved  = 1.0
	confidenceShortName = 0.9
	confidenceHeuristic = 0.75

	provenanceExtracted codegraph.Provenance = "extracted"
	provenanceResolved  codegraph.Provenance = "resolved"
	provenanceHeuristic codegraph.Provenance = "heuristic"
)

func (r *Repository) resolveAllEdges(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, kind, language, path, name, qualified_name, signature, line_start, line_end, content_hash
		FROM graph_nodes`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	nodesByID := make(map[string]codegraph.Node, 256)
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
			return err
		}
		nodesByID[node.ID] = node
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	qualifiedIndex := make(map[string][]string)
	terminalIndex := make(map[string][]string)
	for id, node := range nodesByID {
		if node.QualifiedName != "" {
			qualifiedIndex[node.QualifiedName] = append(qualifiedIndex[node.QualifiedName], id)
		}
		if node.Name != "" {
			terminalIndex[node.Name] = append(terminalIndex[node.Name], id)
		}
	}
	for _, ids := range qualifiedIndex {
		sort.Strings(ids)
	}
	for _, ids := range terminalIndex {
		sort.Strings(ids)
	}

	edgeRows, err := tx.QueryContext(
		ctx,
		`SELECT rowid, source_id, target_id, target_name, kind, provenance FROM graph_edges
		WHERE target_name IS NOT NULL AND target_name <> ''`,
	)
	if err != nil {
		return err
	}
	defer edgeRows.Close()

	type pending struct {
		id         int64
		sourceID   string
		targetID   sql.NullString
		targetName string
		kind       codegraph.EdgeKind
		provenance codegraph.Provenance
	}
	pendingRows := make([]pending, 0)
	for edgeRows.Next() {
		var row pending
		if err := edgeRows.Scan(&row.id, &row.sourceID, &row.targetID, &row.targetName, &row.kind, &row.provenance); err != nil {
			return err
		}
		pendingRows = append(pendingRows, row)
	}
	if err := edgeRows.Err(); err != nil {
		return err
	}
	if err := edgeRows.Close(); err != nil {
		return err
	}

	for _, row := range pendingRows {
		if row.targetID.Valid && row.provenance == provenanceExtracted {
			continue
		}

		targetID, provenance, confidence := r.resolveTargetID(
			row.sourceID,
			strings.TrimSpace(row.targetName),
			row.kind,
			nodesByID,
			qualifiedIndex,
			terminalIndex,
		)
		if targetID == nil {
			provenance = provenanceExtracted
			confidence = confidenceExtracted
		}

		if _, err := tx.ExecContext(
			ctx,
			`UPDATE graph_edges SET target_id = ?, provenance = ?, confidence = ? WHERE rowid = ?`,
			targetID,
			provenance,
			confidence,
			row.id,
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) resolveTargetID(
	sourceID, targetName string,
	kind codegraph.EdgeKind,
	nodesByID map[string]codegraph.Node,
	qualifiedIndex, terminalIndex map[string][]string,
) (*string, codegraph.Provenance, float64) {
	if targetName == "" {
		return nil, provenanceExtracted, confidenceExtracted
	}

	if matched, provenance, confidence := r.resolveTargetByQualified(targetName, qualifiedIndex); matched != "" {
		return &matched, provenance, confidence
	}

	terminal := terminalName(targetName)
	if matched, provenance, confidence := r.resolveTargetByTerminal(terminal, terminalIndex, nodesByID); matched != "" {
		return &matched, provenance, confidence
	}

	if kind == "implements" {
		if matched := r.resolveImplementsHeuristic(sourceID, targetName, nodesByID, qualifiedIndex, terminalIndex); matched != "" {
			return &matched, provenanceHeuristic, confidenceHeuristic
		}
	}

	return nil, provenanceExtracted, confidenceExtracted
}

func (r *Repository) resolveTargetByQualified(
	targetName string,
	qualifiedIndex map[string][]string,
) (string, codegraph.Provenance, float64) {
	matches := qualifiedIndex[targetName]
	if len(matches) == 1 {
		return matches[0], provenanceResolved, confidenceResolved
	}
	return "", "", 0
}

func (r *Repository) resolveTargetByTerminal(
	terminal string,
	terminalIndex map[string][]string,
	nodesByID map[string]codegraph.Node,
) (string, codegraph.Provenance, float64) {
	if terminal == "" {
		return "", "", 0
	}
	matches := terminalIndex[terminal]
	if len(matches) == 1 {
		candidateID := matches[0]
		candidate := nodesByID[candidateID]
		if candidate.Kind == "" {
			return "", "", 0
		}
		return candidateID, provenanceResolved, confidenceShortName
	}
	return "", "", 0
}

func (r *Repository) resolveImplementsHeuristic(
	sourceID string,
	targetName string,
	nodesByID map[string]codegraph.Node,
	qualifiedIndex map[string][]string,
	terminalIndex map[string][]string,
) string {
	source, ok := nodesByID[sourceID]
	if !ok {
		return ""
	}
	if source.Name == "" {
		return ""
	}

	candidates := make([]string, 0)
	if byQName, exists := qualifiedIndex[targetName]; exists {
		candidates = append(candidates, byQName...)
	}
	if byName, exists := terminalIndex[terminalName(targetName)]; exists {
		candidates = append(candidates, byName...)
	}
	if len(candidates) == 0 {
		return ""
	}

	resolved := make([]string, 0)
	for _, candidateID := range candidates {
		candidate := nodesByID[candidateID]
		if candidate.Kind != "interface" {
			continue
		}
		if r.methodsSubsetMatch(source, candidate, nodesByID) {
			resolved = append(resolved, candidateID)
		}
	}

	sort.Strings(resolved)
	if len(resolved) == 1 {
		return resolved[0]
	}
	return ""
}

func (r *Repository) methodsSubsetMatch(
	sourceNode codegraph.Node,
	interfaceNode codegraph.Node,
	nodesByID map[string]codegraph.Node,
) bool {
	sourceOwner := strings.TrimSpace(sourceNode.QualifiedName)
	if sourceOwner == "" {
		return false
	}
	interfaceOwner := strings.TrimSpace(interfaceNode.QualifiedName)
	if interfaceOwner == "" {
		return false
	}

	required := collectMethodNamesByOwner(interfaceOwner, nodesByID)
	sourceMethods := collectMethodNamesByOwner(sourceOwner, nodesByID)
	if len(required) == 0 || len(sourceMethods) == 0 {
		return false
	}

	for method := range required {
		if _, ok := sourceMethods[method]; !ok {
			return false
		}
	}
	return true
}

func collectMethodNamesByOwner(owner string, nodesByID map[string]codegraph.Node) map[string]struct{} {
	methods := make(map[string]struct{}, 8)
	for _, node := range nodesByID {
		if node.Kind != "method" {
			continue
		}
		if owner == "" {
			continue
		}
		if methodOwner(node.QualifiedName) != owner {
			continue
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			continue
		}
		methods[name] = struct{}{}
	}
	return methods
}

func methodOwner(qualifiedName string) string {
	parts := strings.Split(strings.TrimSpace(qualifiedName), ".")
	return parts[len(parts)-1]
}

package graphstore

import (
	"context"
	"fmt"
	"strings"

	"s26.dev/istok-cli/internal/codegraph"
)

func (r *Repository) Path(ctx context.Context, request PathRequest) ([]codegraph.Node, error) {
	paths, err := r.Paths(ctx, PathsRequest(request))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return paths[0], nil
}

func (r *Repository) Paths(ctx context.Context, request PathsRequest) ([][]codegraph.Node, error) {
	if r == nil || r.db == nil {
		return nil, errRepositoryNil
	}

	from := strings.TrimSpace(request.From)
	to := strings.TrimSpace(request.To)
	if from == "" || to == "" {
		return nil, errPathRequestMissing
	}
	maxDepth := request.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	if maxDepth > maxTraversalDepth {
		maxDepth = maxTraversalDepth
	}
	maxResults := normalizeAndLimit(request.MaxResults, defaultMaxPathResult, maxResultLimit)

	if from == to {
		start, err := r.nodeByID(ctx, from)
		if err != nil {
			return nil, err
		}
		if start == nil {
			return nil, nil
		}
		return [][]codegraph.Node{{*start}}, nil
	}

	type queuedPath struct {
		nodeID string
		path   []string
	}
	queue := []queuedPath{{nodeID: from, path: []string{from}}}
	paths := make([][]codegraph.Node, 0, maxResults)
	steps := 0

	for len(queue) > 0 && len(paths) < maxResults && steps < maxTraversalSteps {
		current := queue[0]
		queue = queue[1:]
		steps++

		depth := len(current.path) - 1
		if depth >= maxDepth {
			continue
		}

		nextNodes, err := r.resolvedNeighbors(ctx, current.nodeID)
		if err != nil {
			return nil, err
		}

		for _, neighborID := range nextNodes {
			if nodeInPath(current.path, neighborID) {
				continue
			}
			nextPath := append(make([]string, 0, len(current.path)+1), current.path...)
			nextPath = append(nextPath, neighborID)

			if neighborID == to {
				pathNodes, err := r.nodesByIDs(ctx, nextPath)
				if err != nil {
					return nil, err
				}
				paths = append(paths, pathNodes)
				if len(paths) >= maxResults {
					break
				}
				continue
			}

			queue = append(queue, queuedPath{nodeID: neighborID, path: nextPath})
		}
	}

	return paths, nil
}

func (r *Repository) resolvedNeighbors(ctx context.Context, sourceID string) ([]string, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT target_id
		FROM graph_edges
		WHERE source_id = ? AND target_id IS NOT NULL
		ORDER BY target_id ASC`,
		sourceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return uniqueOrdered(ids), nil
}

func (r *Repository) nodeByID(ctx context.Context, nodeID string) (*codegraph.Node, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, kind, language, path, name, qualified_name, signature, line_start, line_end, content_hash
		FROM graph_nodes
		WHERE id = ?
		LIMIT 1`,
		nodeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

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
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &node, nil
}

func (r *Repository) nodesByIDs(ctx context.Context, nodeIDs []string) ([]codegraph.Node, error) {
	nodes := make([]codegraph.Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, err := r.nodeByID(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		if node == nil {
			return nil, fmt.Errorf("path contains missing node %q", nodeID)
		}
		nodes = append(nodes, *node)
	}
	return nodes, nil
}

func nodeInPath(path []string, nodeID string) bool {
	for _, value := range path {
		if value == nodeID {
			return true
		}
	}
	return false
}

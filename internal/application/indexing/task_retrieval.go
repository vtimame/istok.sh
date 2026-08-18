package indexing

import (
	"context"

	"github.com/vtimame/istok.sh/internal/graphstore"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

type graphLookup struct {
	repository *graphstore.Repository
}

func (g graphLookup) LookupNodesByPathRange(ctx context.Context, request retrieval.GraphPathRangeRequest) ([]retrieval.GraphNode, error) {
	nodes, err := g.repository.LookupNodesByPathRange(ctx, graphstore.GraphPathRangeRequest{
		Path:      request.Path,
		LineStart: request.LineStart,
		LineEnd:   request.LineEnd,
		Limit:     request.Limit,
	})
	if err != nil {
		return nil, err
	}

	result := make([]retrieval.GraphNode, 0, len(nodes))
	for _, node := range nodes {
		symbol := node.QualifiedName
		if symbol == "" {
			symbol = node.Name
		}
		result = append(result, retrieval.GraphNode{
			ID:        node.ID,
			Path:      node.Path,
			Symbol:    symbol,
			LineStart: node.LineStart,
			LineEnd:   node.LineEnd,
		})
	}

	return result, nil
}

func (g graphLookup) NeighborsWithMetadata(ctx context.Context, request retrieval.NeighborsWithMetadataRequest) ([]retrieval.GraphNeighbor, error) {
	neighbors, err := g.repository.NeighborsWithMetadata(ctx, graphstore.NeighborsWithMetadataRequest{SourceID: request.SourceID, Limit: request.Limit})
	if err != nil {
		return nil, err
	}

	result := make([]retrieval.GraphNeighbor, 0, len(neighbors))
	for _, neighbor := range neighbors {
		symbol := neighbor.Node.QualifiedName
		if symbol == "" {
			symbol = neighbor.Node.Name
		}
		result = append(result, retrieval.GraphNeighbor{
			Node: retrieval.GraphNode{
				ID:        neighbor.Node.ID,
				Path:      neighbor.Node.Path,
				Symbol:    symbol,
				LineStart: neighbor.Node.LineStart,
				LineEnd:   neighbor.Node.LineEnd,
			},
			Kind:       string(neighbor.Kind),
			Provenance: string(neighbor.Provenance),
			Confidence: neighbor.Confidence,
		})
	}

	return result, nil
}

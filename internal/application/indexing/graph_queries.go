package indexing

import (
	"context"
	"fmt"
	"strings"

	"s26.dev/istok-cli/internal/codegraph"
	"s26.dev/istok-cli/internal/graphstore"
	"s26.dev/istok-cli/internal/indexing/sidecar"
	"s26.dev/istok-cli/internal/project"
)

func (s *Service) Symbols(ctx context.Context, value project.Project, request SymbolRequest) ([]codegraph.Node, Status, error) {
	if err := validateSymbolRequest(request); err != nil {
		return nil, failedStatus(err), wrapError(failedStatus(err), err)
	}

	limit := graphLimit(request.Limit, defaultGraphLimit)
	return withCurrentGraph(s, ctx, value, func(ctx context.Context, graph *graphstore.Repository) ([]codegraph.Node, error) {
		return graph.LookupSymbols(ctx, graphstore.LookupSymbolsRequest{
			QualifiedName: strings.TrimSpace(request.Name),
			Name:          strings.TrimSpace(request.Name),
			Limit:         limit,
		})
	})
}

func (s *Service) Neighbors(ctx context.Context, value project.Project, request NeighborsRequest) ([]GraphNeighbor, Status, error) {
	if err := validateNeighborsRequest(request); err != nil {
		return nil, failedStatus(err), wrapError(failedStatus(err), err)
	}

	limit := graphLimit(request.Limit, defaultGraphLimit)
	return withCurrentGraph(s, ctx, value, func(ctx context.Context, graph *graphstore.Repository) ([]GraphNeighbor, error) {
		sources, err := graph.LookupSymbols(ctx, graphstore.LookupSymbolsRequest{
			QualifiedName: strings.TrimSpace(request.Name),
			Name:          strings.TrimSpace(request.Name),
			Limit:         maxGraphLimit,
		})
		if err != nil {
			return nil, err
		}

		neighbors := make([]GraphNeighbor, 0, limit)
		for _, source := range sources {
			if len(neighbors) == limit {
				break
			}

			matches, err := graph.NeighborsWithMetadata(ctx, graphstore.NeighborsWithMetadataRequest{
				SourceID: source.ID,
				Kinds:    append([]codegraph.EdgeKind(nil), request.Kinds...),
				Limit:    limit - len(neighbors),
			})
			if err != nil {
				return nil, err
			}

			for _, match := range matches {
				neighbors = append(neighbors, GraphNeighbor{
					Source:       source,
					Target:       match.Node,
					Kind:         match.Kind,
					Provenance:   match.Provenance,
					Confidence:   match.Confidence,
					EvidencePath: match.EvidencePath,
					EvidenceLine: match.EvidenceLine,
				})
			}
		}

		return neighbors, nil
	})
}

func (s *Service) Paths(ctx context.Context, value project.Project, request PathRequest) ([]GraphPath, Status, error) {
	if err := validatePathRequest(request); err != nil {
		return nil, failedStatus(err), wrapError(failedStatus(err), err)
	}

	limit := graphLimit(request.Limit, defaultPathLimit)
	return withCurrentGraph(s, ctx, value, func(ctx context.Context, graph *graphstore.Repository) ([]GraphPath, error) {
		from, err := graph.LookupSymbols(ctx, graphstore.LookupSymbolsRequest{
			QualifiedName: strings.TrimSpace(request.From),
			Name:          strings.TrimSpace(request.From),
			Limit:         maxGraphLimit,
		})
		if err != nil {
			return nil, err
		}
		to, err := graph.LookupSymbols(ctx, graphstore.LookupSymbolsRequest{
			QualifiedName: strings.TrimSpace(request.To),
			Name:          strings.TrimSpace(request.To),
			Limit:         maxGraphLimit,
		})
		if err != nil {
			return nil, err
		}

		paths := make([]GraphPath, 0, limit)
		for _, source := range from {
			for _, target := range to {
				if len(paths) == limit {
					return paths, nil
				}

				matches, err := graph.Paths(ctx, graphstore.PathsRequest{
					From:       source.ID,
					To:         target.ID,
					MaxDepth:   request.MaxDepth,
					MaxResults: limit - len(paths),
				})
				if err != nil {
					return nil, err
				}
				for _, nodes := range matches {
					paths = append(paths, GraphPath{Nodes: nodes})
				}
			}
		}

		return paths, nil
	})
}

func withCurrentGraph[T any](s *Service, ctx context.Context, value project.Project, query func(context.Context, *graphstore.Repository) ([]T, error)) ([]T, Status, error) {
	status, err := s.EnsureFresh(ctx, value)
	if err != nil {
		return nil, status, err
	}
	if !readableStatus(status) {
		err := fmt.Errorf("index is not readable in state %q", status.State)
		return nil, status, wrapError(status, err)
	}

	indexedProject, err := s.openProject(ctx, value)
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	defer indexedProject.Close()

	generation, err := indexedProject.Current(ctx)
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	defer generation.Close()

	state, err := generation.State()
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	status = statusFromState(state)
	if !readableStatus(status) {
		err := fmt.Errorf("index became unreadable in state %q", status.State)
		return nil, status, wrapError(status, err)
	}

	values, err := query(ctx, generation.Graph())
	if err != nil {
		return nil, status, wrapError(status, err)
	}

	return values, status, nil
}

func readableStatus(status Status) bool {
	return status.State == sidecar.StateReady || status.State == sidecar.StateDegraded
}

func validateSymbolRequest(request SymbolRequest) error {
	if strings.TrimSpace(request.Name) == "" {
		return fmt.Errorf("symbol name is required")
	}
	if request.Limit < 0 {
		return fmt.Errorf("symbol limit must not be negative")
	}
	if request.Limit > maxGraphLimit {
		return fmt.Errorf("symbol limit must not exceed %d", maxGraphLimit)
	}
	return nil
}

func validateNeighborsRequest(request NeighborsRequest) error {
	if strings.TrimSpace(request.Name) == "" {
		return fmt.Errorf("neighbor source name is required")
	}
	if request.Limit < 0 {
		return fmt.Errorf("neighbor limit must not be negative")
	}
	if request.Limit > maxGraphLimit {
		return fmt.Errorf("neighbor limit must not exceed %d", maxGraphLimit)
	}
	for _, kind := range request.Kinds {
		if !validGraphEdgeKind(kind) {
			return fmt.Errorf("invalid graph edge kind %q", kind)
		}
	}
	return nil
}

func validatePathRequest(request PathRequest) error {
	if strings.TrimSpace(request.From) == "" || strings.TrimSpace(request.To) == "" {
		return fmt.Errorf("path endpoints are required")
	}
	if request.MaxDepth < 0 {
		return fmt.Errorf("path max depth must not be negative")
	}
	if request.Limit < 0 {
		return fmt.Errorf("path limit must not be negative")
	}
	if request.Limit > maxGraphLimit {
		return fmt.Errorf("path limit must not exceed %d", maxGraphLimit)
	}
	return nil
}

func graphLimit(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func validGraphEdgeKind(kind codegraph.EdgeKind) bool {
	switch kind {
	case codegraph.EdgeContains,
		codegraph.EdgeImports,
		codegraph.EdgeCalls,
		codegraph.EdgeReferences,
		codegraph.EdgeInherits,
		codegraph.EdgeImplements:
		return true
	default:
		return false
	}
}

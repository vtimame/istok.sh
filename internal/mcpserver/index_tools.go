package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	"github.com/vtimame/istok.sh/internal/codegraph"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

func addIndexTools(server *mcp.Server, projects *project.Service, indexes *indexingapp.Service, root string) {
	addIndexStatusTool(server, projects, indexes, root)
	addIndexRebuildTool(server, projects, indexes, root)
	addSearchTool(server, projects, indexes, root)
	addGraphSymbolTool(server, projects, indexes, root)
	addGraphNeighborsTool(server, projects, indexes, root)
	addGraphPathTool(server, projects, indexes, root)
}

func addIndexStatusTool(server *mcp.Server, projects *project.Service, indexes *indexingapp.Service, root string) {
	mcp.AddTool(server, tool("index_status", "Inspect index state for the fixed current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, _ indexEmptyInput) (*mcp.CallToolResult, indexStatusResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), indexStatusResult{SchemaVersion: indexToolSchemaVersion, Error: toolError(err)}, nil
		}

		status, err := indexes.Status(ctx, current)
		return indexStatusCall(current, status, err)
	})
}

func addIndexRebuildTool(server *mcp.Server, projects *project.Service, indexes *indexingapp.Service, root string) {
	mcp.AddTool(server, tool("index_rebuild", "Force rebuild the index for the fixed current project.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, _ indexEmptyInput) (*mcp.CallToolResult, indexStatusResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), indexStatusResult{SchemaVersion: indexToolSchemaVersion, Error: toolError(err)}, nil
		}

		status, err := indexes.Rebuild(ctx, current)
		return indexStatusCall(current, status, err)
	})
}

func addSearchTool(server *mcp.Server, projects *project.Service, indexes *indexingapp.Service, root string) {
	mcp.AddTool(server, tool("search", "Search the fresh index for the fixed current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, input indexSearchInput) (*mcp.CallToolResult, indexSearchResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), indexSearchResult{SchemaVersion: indexToolSchemaVersion, ContractVersion: retrieval.ContractVersion, Results: []retrieval.SearchResult{}, Error: toolError(err)}, nil
		}

		results, status, err := indexes.Search(ctx, current, retrieval.SearchRequest{Query: input.Query, Limit: input.Limit})
		if results == nil {
			results = []retrieval.SearchResult{}
		}

		result := indexSearchResult{SchemaVersion: indexToolSchemaVersion, Project: &current, Status: &status, ContractVersion: retrieval.ContractVersion, Results: results}
		if err != nil {
			result.Error = toolError(err)
			return errorTool(err), result, nil
		}

		return nil, result, nil
	})
}

func addGraphSymbolTool(server *mcp.Server, projects *project.Service, indexes *indexingapp.Service, root string) {
	mcp.AddTool(server, tool("graph_symbol", "Find graph symbols in the fixed current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, input graphSymbolInput) (*mcp.CallToolResult, graphSymbolResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), graphSymbolResult{SchemaVersion: indexToolSchemaVersion, ContractVersion: codegraph.ContractVersion, Symbols: []codegraph.Node{}, Error: toolError(err)}, nil
		}

		symbols, status, err := indexes.Symbols(ctx, current, indexingapp.SymbolRequest{Name: input.Name, Limit: input.Limit})
		if symbols == nil {
			symbols = []codegraph.Node{}
		}

		result := graphSymbolResult{SchemaVersion: indexToolSchemaVersion, Project: &current, Status: &status, ContractVersion: codegraph.ContractVersion, Symbols: symbols}
		if err != nil {
			result.Error = toolError(err)
			return errorTool(err), result, nil
		}

		return nil, result, nil
	})
}

func addGraphNeighborsTool(server *mcp.Server, projects *project.Service, indexes *indexingapp.Service, root string) {
	mcp.AddTool(server, tool("graph_neighbors", "Find graph neighbors in the fixed current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, input graphNeighborsInput) (*mcp.CallToolResult, graphNeighborsResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), graphNeighborsResult{SchemaVersion: indexToolSchemaVersion, ContractVersion: codegraph.ContractVersion, Neighbors: []indexingapp.GraphNeighbor{}, Error: toolError(err)}, nil
		}

		neighbors, status, err := indexes.Neighbors(ctx, current, indexingapp.NeighborsRequest{Name: input.Name, Kinds: input.Kinds, Limit: input.Limit})
		if neighbors == nil {
			neighbors = []indexingapp.GraphNeighbor{}
		}

		result := graphNeighborsResult{SchemaVersion: indexToolSchemaVersion, Project: &current, Status: &status, ContractVersion: codegraph.ContractVersion, Neighbors: neighbors}
		if err != nil {
			result.Error = toolError(err)
			return errorTool(err), result, nil
		}

		return nil, result, nil
	})
}

func addGraphPathTool(server *mcp.Server, projects *project.Service, indexes *indexingapp.Service, root string) {
	mcp.AddTool(server, tool("graph_path", "Find bounded graph paths in the fixed current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, input graphPathInput) (*mcp.CallToolResult, graphPathResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), graphPathResult{SchemaVersion: indexToolSchemaVersion, ContractVersion: codegraph.ContractVersion, Paths: []indexingapp.GraphPath{}, Error: toolError(err)}, nil
		}

		paths, status, err := indexes.Paths(ctx, current, indexingapp.PathRequest{From: input.From, To: input.To, MaxDepth: input.MaxDepth, Limit: input.Limit})
		if paths == nil {
			paths = []indexingapp.GraphPath{}
		}

		result := graphPathResult{SchemaVersion: indexToolSchemaVersion, Project: &current, Status: &status, ContractVersion: codegraph.ContractVersion, Paths: paths}
		if err != nil {
			result.Error = toolError(err)
			return errorTool(err), result, nil
		}

		return nil, result, nil
	})
}

func indexStatusCall(current project.Project, status indexingapp.Status, err error) (*mcp.CallToolResult, indexStatusResult, error) {
	result := indexStatusResult{SchemaVersion: indexToolSchemaVersion, Project: &current, Status: &status}
	if err != nil {
		result.Error = toolError(err)
		return errorTool(err), result, nil
	}

	return nil, result, nil
}

package graphstore

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vtimame/istok.sh/internal/codegraph"
)

func normalizeAndSortFileGraphs(files []codegraph.FileGraph) ([]codegraph.FileGraph, error) {
	normalized := make([]codegraph.FileGraph, 0, len(files))
	seenPaths := make(map[string]struct{}, len(files))

	for _, file := range files {
		path := normalizePath(file.Path)
		if path == "" {
			return nil, fmt.Errorf("file path is required")
		}
		if _, exists := seenPaths[path]; exists {
			return nil, fmt.Errorf("duplicate file path %q", path)
		}
		seenPaths[path] = struct{}{}

		file.Path = path
		file.Language = strings.TrimSpace(file.Language)
		file.ContentHash = strings.TrimSpace(file.ContentHash)
		if file.ContentHash == "" {
			return nil, fmt.Errorf("content hash is required for %q", path)
		}

		nodes := make([]codegraph.Node, 0, len(file.Nodes))
		nodeIDs := make(map[string]struct{}, len(file.Nodes))
		for _, node := range file.Nodes {
			node.Path = path
			node.Kind = codegraph.NodeKind(strings.TrimSpace(string(node.Kind)))
			if !validNodeKind(node.Kind) {
				return nil, fmt.Errorf("invalid node kind %q in %q", node.Kind, path)
			}
			node.Language = strings.TrimSpace(node.Language)
			if node.Language == "" {
				node.Language = file.Language
			}
			node.Name = strings.TrimSpace(node.Name)
			node.QualifiedName = strings.TrimSpace(node.QualifiedName)
			node.Signature = strings.TrimSpace(node.Signature)
			node.ContentHash = strings.TrimSpace(node.ContentHash)
			if node.ContentHash == "" {
				node.ContentHash = file.ContentHash
			}
			if node.ID == "" {
				return nil, fmt.Errorf("node id is required in %q", path)
			}
			if node.LineStart <= 0 || node.LineEnd < node.LineStart {
				return nil, fmt.Errorf("invalid node range for %q", node.ID)
			}
			if _, exists := nodeIDs[node.ID]; exists {
				return nil, fmt.Errorf("duplicate node id %q in %q", node.ID, path)
			}
			nodeIDs[node.ID] = struct{}{}
			nodes = append(nodes, node)
		}
		sort.Slice(nodes, func(i, j int) bool {
			left, right := nodes[i], nodes[j]
			if left.LineStart != right.LineStart {
				return left.LineStart < right.LineStart
			}
			if left.LineEnd != right.LineEnd {
				return left.LineEnd < right.LineEnd
			}
			if left.QualifiedName != right.QualifiedName {
				return left.QualifiedName < right.QualifiedName
			}
			if left.ID != right.ID {
				return left.ID < right.ID
			}
			return left.Name < right.Name
		})

		edges := make([]codegraph.Edge, 0, len(file.Edges))
		for _, edge := range file.Edges {
			edge.SourceID = strings.TrimSpace(edge.SourceID)
			edge.TargetID = strings.TrimSpace(edge.TargetID)
			edge.TargetName = strings.TrimSpace(edge.TargetName)
			if edge.TargetName == "" {
				return nil, fmt.Errorf("edge target name is required in %q", path)
			}
			edge.Kind = codegraph.EdgeKind(strings.TrimSpace(string(edge.Kind)))
			if !validEdgeKind(edge.Kind) {
				return nil, fmt.Errorf("invalid edge kind %q in %q", edge.Kind, path)
			}
			if edge.Confidence < 0 || edge.Confidence > 1 {
				return nil, fmt.Errorf("invalid edge confidence %v in %q", edge.Confidence, path)
			}
			edge.Provenance = codegraph.Provenance(strings.TrimSpace(string(edge.Provenance)))
			if edge.Provenance == "" {
				edge.Provenance = codegraph.ProvenanceExtracted
			}
			if edge.Provenance != codegraph.ProvenanceExtracted {
				return nil, fmt.Errorf("replacement edge must have extracted provenance in %q", path)
			}
			if edge.EvidenceLine <= 0 {
				return nil, fmt.Errorf("edge evidence line is required in %q", path)
			}
			edge.EvidencePath = normalizePath(edge.EvidencePath)
			if edge.EvidencePath == "" {
				edge.EvidencePath = path
			}
			edges = append(edges, edge)
		}
		sort.Slice(edges, func(i, j int) bool {
			left, right := edges[i], edges[j]
			if left.SourceID != right.SourceID {
				return left.SourceID < right.SourceID
			}
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			if left.TargetName != right.TargetName {
				return left.TargetName < right.TargetName
			}
			if left.EvidencePath != right.EvidencePath {
				return left.EvidencePath < right.EvidencePath
			}
			return left.EvidenceLine < right.EvidenceLine
		})

		diagnostics := make([]codegraph.Diagnostic, 0, len(file.Diagnostics))
		for _, diagnostic := range file.Diagnostics {
			diagnostic.Path = normalizePath(diagnostic.Path)
			if diagnostic.Path == "" {
				diagnostic.Path = path
			}
			diagnostic.Language = strings.TrimSpace(diagnostic.Language)
			diagnostic.Message = strings.TrimSpace(diagnostic.Message)
			diagnostics = append(diagnostics, diagnostic)
		}
		sort.Slice(diagnostics, func(i, j int) bool {
			left, right := diagnostics[i], diagnostics[j]
			if left.Path != right.Path {
				return left.Path < right.Path
			}
			if left.Line != right.Line {
				return left.Line < right.Line
			}
			return left.Message < right.Message
		})

		file.Nodes = nodes
		file.Edges = edges
		file.Diagnostics = diagnostics
		normalized = append(normalized, file)
	}

	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Path != normalized[j].Path {
			return normalized[i].Path < normalized[j].Path
		}
		return normalized[i].ContentHash < normalized[j].ContentHash
	})

	return normalized, nil
}

func uniqueOrdered(values []string) []string {
	if len(values) == 0 {
		return values
	}
	sort.Strings(values)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func terminalName(value string) string {
	parts := strings.Split(strings.TrimSpace(value), ".")
	return parts[len(parts)-1]
}

func validNodeKind(kind codegraph.NodeKind) bool {
	switch kind {
	case codegraph.NodeFile,
		codegraph.NodeModule,
		codegraph.NodeType,
		codegraph.NodeInterface,
		codegraph.NodeFunction,
		codegraph.NodeMethod:
		return true
	default:
		return false
	}
}

func validEdgeKind(kind codegraph.EdgeKind) bool {
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

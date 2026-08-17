package codegraph

import "sort"

const ContractVersion = "istok.graph.v1"

type NodeKind string
type EdgeKind string
type Provenance string

const (
	NodeFile      NodeKind = "file"
	NodeModule    NodeKind = "module"
	NodeType      NodeKind = "type"
	NodeInterface NodeKind = "interface"
	NodeFunction  NodeKind = "function"
	NodeMethod    NodeKind = "method"

	EdgeContains   EdgeKind = "contains"
	EdgeImports    EdgeKind = "imports"
	EdgeCalls      EdgeKind = "calls"
	EdgeReferences EdgeKind = "references"
	EdgeInherits   EdgeKind = "inherits"
	EdgeImplements EdgeKind = "implements"

	ProvenanceExtracted Provenance = "extracted"
	ProvenanceResolved  Provenance = "resolved"
	ProvenanceHeuristic Provenance = "heuristic"
)

type Node struct {
	ID            string   `json:"id"`
	Kind          NodeKind `json:"kind"`
	Language      string   `json:"language"`
	Path          string   `json:"path"`
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualified_name"`
	Signature     string   `json:"signature"`
	LineStart     int      `json:"line_start"`
	LineEnd       int      `json:"line_end"`
	ContentHash   string   `json:"content_hash"`
}

type Edge struct {
	SourceID     string     `json:"source_id"`
	TargetID     string     `json:"target_id,omitempty"`
	TargetName   string     `json:"target_name"`
	Kind         EdgeKind   `json:"kind"`
	Provenance   Provenance `json:"provenance"`
	Confidence   float64    `json:"confidence"`
	EvidencePath string     `json:"evidence_path"`
	EvidenceLine int        `json:"evidence_line"`
}

type Diagnostic struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

type SymbolChunk struct {
	Symbol    string
	LineStart int
	LineEnd   int
}

type FileGraph struct {
	Path        string       `json:"path"`
	Language    string       `json:"language"`
	ContentHash string       `json:"content_hash"`
	Nodes       []Node       `json:"nodes"`
	Edges       []Edge       `json:"edges"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

func (g FileGraph) Symbols() []SymbolChunk {
	symbols := make([]SymbolChunk, 0)
	for _, node := range g.Nodes {
		if node.Kind == NodeFile || node.Kind == NodeModule {
			continue
		}

		symbol := node.QualifiedName
		if symbol == "" {
			symbol = node.Name
		}
		if symbol == "" {
			continue
		}

		symbols = append(symbols, SymbolChunk{
			Symbol:    symbol,
			LineStart: node.LineStart,
			LineEnd:   node.LineEnd,
		})
	}
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].LineStart != symbols[j].LineStart {
			return symbols[i].LineStart < symbols[j].LineStart
		}
		if symbols[i].LineEnd != symbols[j].LineEnd {
			return symbols[i].LineEnd < symbols[j].LineEnd
		}
		return symbols[i].Symbol < symbols[j].Symbol
	})

	return symbols
}

func (g FileGraph) SymbolChunks() []SymbolChunk {
	return g.Symbols()
}

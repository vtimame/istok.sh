package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

type goldenCase struct {
	name             string
	language         string
	path             string
	expectKinds      []NodeKind
	expectEdgeKinds  []EdgeKind
	mustContainQname map[NodeKind][]string
	mustContainEdge  map[EdgeKind]string
	minNodes         int
}

func TestNewAnalyzerSupports(t *testing.T) {
	an := NewAnalyzer()

	for _, language := range []string{"go", "typescript", "tsx", "javascript", "jsx"} {
		t.Run(language, func(t *testing.T) {
			if !an.Supports(language) {
				t.Fatalf("expected language %q to be supported", language)
			}
		})
	}

	if an.Supports("python") {
		t.Fatalf("python should be unsupported")
	}
}

func TestAnalyzeUnsupportedLanguage(t *testing.T) {
	an := NewAnalyzer()
	if _, err := an.Analyze("sample.txt", "python", []byte(""), "abc"); err == nil {
		t.Fatalf("expected error for unsupported language")
	}
}

func TestAnalyzeAllLanguages(t *testing.T) {
	an := NewAnalyzer()
	cases := []goldenCase{
		{
			name:        "go",
			language:    "go",
			path:        filepath.Join("testdata", "codegraph", "go", "sample.go"),
			minNodes:    6, // file, module, interface, type, type, type, function, method
			expectKinds: []NodeKind{NodeFile, NodeModule, NodeType, NodeInterface, NodeFunction, NodeMethod},
			expectEdgeKinds: []EdgeKind{
				EdgeContains,
				EdgeImports,
				EdgeCalls,
				EdgeReferences,
			},
			mustContainQname: map[NodeKind][]string{
				NodeType:      {"billing.Repository"},
				NodeInterface: {"billing.Billing"},
				NodeFunction:  {"billing.NewProcessor"},
				NodeMethod:    {"billing.Processor.Pay"},
			},
			mustContainEdge: map[EdgeKind]string{
				EdgeImports: "github.com/example/ext",
			},
		},
		{
			name:        "typescript",
			language:    "typescript",
			path:        filepath.Join("testdata", "codegraph", "typescript", "sample.ts"),
			minNodes:    6, // file, module, interface, two classes, function
			expectKinds: []NodeKind{NodeFile, NodeModule, NodeInterface, NodeType, NodeFunction},
			expectEdgeKinds: []EdgeKind{
				EdgeContains,
				EdgeImports,
				EdgeCalls,
				EdgeInherits,
				EdgeImplements,
			},
			mustContainQname: map[NodeKind][]string{
				NodeInterface: {"sample.BillingService"},
				NodeType: {
					"sample.CheckoutService",
					"sample.Service",
				},
				NodeFunction: {"sample.process"},
			},
			mustContainEdge: map[EdgeKind]string{
				EdgeInherits:   "Service",
				EdgeImplements: "BillingService",
				EdgeCalls:      "format",
				EdgeContains:   "sample.Service",
			},
		},
		{
			name:        "tsx",
			language:    "tsx",
			path:        filepath.Join("testdata", "codegraph", "tsx", "sample.tsx"),
			minNodes:    5, // file, module, interface, type, function, method
			expectKinds: []NodeKind{NodeFile, NodeModule, NodeInterface, NodeType, NodeFunction, NodeMethod},
			expectEdgeKinds: []EdgeKind{
				EdgeContains,
				EdgeCalls,
				EdgeReferences,
				EdgeImplements,
			},
			mustContainQname: map[NodeKind][]string{
				NodeInterface: {"sample.WidgetProps"},
				NodeType:      {"sample.Widget"},
				NodeFunction:  {"sample.create"},
			},
			mustContainEdge: map[EdgeKind]string{
				EdgeCalls: "Widget",
			},
		},
		{
			name:        "javascript",
			language:    "javascript",
			path:        filepath.Join("testdata", "codegraph", "javascript", "sample.js"),
			minNodes:    4, // file, module, class, function
			expectKinds: []NodeKind{NodeFile, NodeModule, NodeType, NodeFunction},
			expectEdgeKinds: []EdgeKind{
				EdgeContains,
				EdgeImports,
				EdgeCalls,
				EdgeReferences,
			},
			mustContainQname: map[NodeKind][]string{
				NodeType:     {"sample.Service"},
				NodeFunction: {"sample.run"},
			},
			mustContainEdge: map[EdgeKind]string{
				EdgeCalls: "Service",
			},
		},
		{
			name:        "jsx",
			language:    "jsx",
			path:        filepath.Join("testdata", "codegraph", "jsx", "sample.jsx"),
			minNodes:    4, // file, module, class, function
			expectKinds: []NodeKind{NodeFile, NodeModule, NodeType, NodeFunction},
			expectEdgeKinds: []EdgeKind{
				EdgeContains,
				EdgeCalls,
				EdgeReferences,
			},
			mustContainQname: map[NodeKind][]string{
				NodeType:     {"sample.View"},
				NodeFunction: {"sample.renderLabel"},
			},
			mustContainEdge: map[EdgeKind]string{
				EdgeCalls: "renderLabel",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content, err := readFixture(tc.path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			graph, err := an.Analyze(tc.path, tc.language, content, sha256Hex(content))
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			if len(graph.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %#v", graph.Diagnostics)
			}
			if len(graph.Nodes) < tc.minNodes {
				t.Fatalf("expected at least %d nodes, got %d", tc.minNodes, len(graph.Nodes))
			}
			if len(graph.Edges) == 0 {
				t.Fatalf("expected edges")
			}

			nodeKinds := make(map[NodeKind]struct{})
			for _, node := range graph.Nodes {
				nodeKinds[node.Kind] = struct{}{}
				if node.LineStart < 1 || node.LineEnd < node.LineStart {
					t.Fatalf("invalid node range for %s: start=%d end=%d", node.QualifiedName, node.LineStart, node.LineEnd)
				}
			}
			for _, kind := range tc.expectKinds {
				if _, ok := nodeKinds[kind]; !ok {
					t.Fatalf("missing node kind %q", kind)
				}
			}

			edgeKinds := make(map[EdgeKind]struct{})
			for _, edge := range graph.Edges {
				edgeKinds[edge.Kind] = struct{}{}
				if edge.EvidenceLine <= 0 {
					t.Fatalf("invalid evidence line for %q edge", edge.Kind)
				}
				if edge.Provenance != ProvenanceExtracted {
					t.Fatalf("edge provenance should be %q, got %q", ProvenanceExtracted, edge.Provenance)
				}
				if edge.Confidence != 1.0 {
					t.Fatalf("edge confidence should be 1.0, got %v", edge.Confidence)
				}
				if edge.SourceID == "" {
					t.Fatalf("edge has empty SourceID")
				}
				if edge.TargetName == "" {
					t.Fatalf("edge has empty TargetName")
				}
			}
			for _, kind := range tc.expectEdgeKinds {
				if _, ok := edgeKinds[kind]; !ok {
					t.Fatalf("missing edge kind %q", kind)
				}
			}

			qnames := make(map[NodeKind]map[string]struct{})
			for _, node := range graph.Nodes {
				if node.QualifiedName == "" {
					continue
				}
				if _, ok := qnames[node.Kind]; !ok {
					qnames[node.Kind] = map[string]struct{}{}
				}
				qnames[node.Kind][node.QualifiedName] = struct{}{}
			}
			for kind, expectedQnames := range tc.mustContainQname {
				if got, ok := qnames[kind]; !ok {
					t.Fatalf("missing nodes of kind %q", kind)
				} else if len(got) == 0 {
					t.Fatalf("missing nodes of kind %q", kind)
				}
				for _, qname := range expectedQnames {
					if _, ok := qnames[kind][qname]; !ok {
						t.Fatalf("missing qname %q for kind %q", qname, kind)
					}
				}
			}

			for edgeKind, target := range tc.mustContainEdge {
				found := false
				for _, edge := range graph.Edges {
					if edge.Kind == edgeKind && edge.TargetName == target {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing expected edge target %q for kind %q", target, edgeKind)
				}
			}
		})
	}
}

func TestAnalyzeParseError(t *testing.T) {
	an := NewAnalyzer()
	path := filepath.Join("testdata", "codegraph", "javascript", "bad.js")
	content, err := readFixture(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	graph, err := an.Analyze(path, "javascript", content, sha256Hex(content))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(graph.Diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(graph.Diagnostics))
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("expected only file node, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 0 {
		t.Fatalf("expected no edges on parse error")
	}
	if graph.Nodes[0].Kind != NodeFile {
		t.Fatalf("expected file node")
	}
}

func TestAnalyzeStableIDsOnBodyOnlyEdit(t *testing.T) {
	an := NewAnalyzer()
	path := filepath.Join("testdata", "codegraph", "go", "sample_body_only_a.go")
	a, err := readFixture(path)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	b, err := readFixture(filepath.Join("testdata", "codegraph", "go", "sample_body_only_b.go"))
	if err != nil {
		t.Fatalf("read changed: %v", err)
	}

	baseline, err := an.Analyze(path, "go", a, sha256Hex(a))
	if err != nil {
		t.Fatalf("analyze baseline: %v", err)
	}
	changed, err := an.Analyze(path, "go", b, sha256Hex(b))
	if err != nil {
		t.Fatalf("analyze changed: %v", err)
	}

	baselineIDs := make(map[string]string)
	for _, node := range baseline.Nodes {
		if node.Kind == NodeFile || node.Kind == NodeModule {
			continue
		}
		baselineIDs[string(node.Kind)+"|"+node.QualifiedName] = node.ID
	}

	for _, node := range changed.Nodes {
		if node.Kind == NodeFile || node.Kind == NodeModule {
			continue
		}
		key := string(node.Kind) + "|" + node.QualifiedName
		id, ok := baselineIDs[key]
		if !ok || id == "" || id != node.ID {
			t.Fatalf("unstable id for %q: %q", key, node.ID)
		}
	}
}

func TestFileGraphSymbols(t *testing.T) {
	an := NewAnalyzer()
	path := filepath.Join("testdata", "codegraph", "typescript", "sample.ts")
	content, err := readFixture(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	graph, err := an.Analyze(path, "typescript", content, sha256Hex(content))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	symbols := graph.Symbols()
	if len(symbols) == 0 {
		t.Fatalf("expected symbols")
	}

	for _, symbol := range symbols {
		if symbol.LineStart < 1 || symbol.LineEnd < symbol.LineStart {
			t.Fatalf("invalid symbol range: %#v", symbol)
		}
	}

	chunks := graph.SymbolChunks()
	if len(chunks) != len(symbols) {
		t.Fatalf("symbols/chunks mismatch")
	}
}

func TestSymbolsAreSortedAndExcludeFileModule(t *testing.T) {
	an := NewAnalyzer()
	path := filepath.Join("testdata", "codegraph", "go", "sample.go")
	content, err := readFixture(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	graph, err := an.Analyze(path, "go", content, sha256Hex(content))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	symbols := graph.Symbols()
	for i := 1; i < len(symbols); i++ {
		if symbols[i-1].LineStart > symbols[i].LineStart {
			t.Fatalf("symbols not sorted")
		}
	}

	for _, node := range graph.Nodes {
		if node.Kind == NodeFile || node.Kind == NodeModule {
			continue
		}
		found := false
		for _, symbol := range symbols {
			if symbol.Symbol == node.QualifiedName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing symbol for %q", node.QualifiedName)
		}
	}
}

func readFixture(path string) ([]byte, error) {
	if _, err := os.Stat(path); err == nil {
		return os.ReadFile(path)
	}
	return os.ReadFile(filepath.Join("..", "..", path))
}

func sha256Hex(value []byte) string {
	h := sha256.Sum256(value)
	return hex.EncodeToString(h[:])
}

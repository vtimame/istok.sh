package codegraph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"s26.dev/istok-cli/internal/retrieval"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type Analyzer struct{}

func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

func (a *Analyzer) Supports(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "go", "typescript", "tsx", "javascript", "jsx":
		return true
	default:
		return false
	}
}

func (a *Analyzer) Analyze(path, language string, content []byte, contentHash string) (FileGraph, error) {
	if a == nil {
		return FileGraph{}, fmt.Errorf("analyzer is nil")
	}
	if !a.Supports(language) {
		return FileGraph{}, fmt.Errorf("unsupported language %q", language)
	}
	if path == "" {
		return FileGraph{}, fmt.Errorf("empty path")
	}
	if contentHash == "" {
		return FileGraph{}, fmt.Errorf("empty content hash")
	}

	normalizedLanguage := strings.ToLower(strings.TrimSpace(language))
	normalizedPath := retrieval.NormalizePath(path)
	if normalizedPath == "" {
		return FileGraph{}, fmt.Errorf("empty normalized path")
	}

	sourceLines := contentLineCount(content)
	fileNode := Node{
		Kind:          NodeFile,
		Language:      normalizedLanguage,
		Path:          normalizedPath,
		Name:          filepath.Base(normalizedPath),
		QualifiedName: filepath.Base(normalizedPath),
		LineStart:     1,
		LineEnd:       sourceLines,
		ContentHash:   contentHash,
	}
	fileNode.ID = deterministicNodeID(fileNode.Path, fileNode.Kind, fileNode.QualifiedName, fileNode.Signature)
	graph := FileGraph{
		Path:        normalizedPath,
		Language:    normalizedLanguage,
		ContentHash: contentHash,
		Nodes:       []Node{fileNode},
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()

	lang, err := parserLanguage(normalizedLanguage)
	if err != nil {
		return FileGraph{}, err
	}
	if err = parser.SetLanguage(lang); err != nil {
		return FileGraph{}, fmt.Errorf("set parser language %q: %w", normalizedLanguage, err)
	}

	t := parser.Parse(content, nil)
	if t == nil {
		return FileGraph{}, fmt.Errorf("parse tree is nil")
	}
	defer t.Close()

	root := t.RootNode()
	if root == nil {
		return FileGraph{}, fmt.Errorf("parse tree root is nil")
	}

	if root.HasError() {
		graph.Diagnostics = []Diagnostic{{
			Path:     normalizedPath,
			Language: normalizedLanguage,
			Line:     firstErrorLine(root),
			Message:  "syntax error",
		}}
		return graph, nil
	}

	builder := newGraphBuilder(normalizedPath, normalizedLanguage, contentHash, content)
	fileIndex := builder.addNode(fileNode)

	moduleName := deriveModuleName(normalizedPath, normalizedLanguage, root, content)
	moduleNode := Node{
		Kind:          NodeModule,
		Language:      normalizedLanguage,
		Path:          normalizedPath,
		Name:          moduleName,
		QualifiedName: moduleName,
		LineStart:     1,
		LineEnd:       sourceLines,
		ContentHash:   contentHash,
	}
	moduleIndex := builder.addNode(moduleNode)
	builder.setModuleName(moduleName)
	builder.addEdge(fileIndex, EdgeContains, moduleName, lineFromNode(root), root)

	switch normalizedLanguage {
	case "go":
		analyzeGoTree(root, &moduleName, fileIndex, moduleIndex, builder)
	default:
		analyzeJSishTree(root, moduleName, fileIndex, moduleIndex, builder)
	}

	nodeIDs := builder.assignNodeIDs()
	graph.Nodes = builder.orderedNodes()
	graph.Edges = builder.resolveEdges(nodeIDs)

	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].SourceID != graph.Edges[j].SourceID {
			return graph.Edges[i].SourceID < graph.Edges[j].SourceID
		}
		if graph.Edges[i].Kind != graph.Edges[j].Kind {
			return graph.Edges[i].Kind < graph.Edges[j].Kind
		}
		if graph.Edges[i].TargetName != graph.Edges[j].TargetName {
			return graph.Edges[i].TargetName < graph.Edges[j].TargetName
		}
		return graph.Edges[i].EvidenceLine < graph.Edges[j].EvidenceLine
	})

	return graph, nil
}

func parserLanguage(language string) (*tree_sitter.Language, error) {
	switch language {
	case "go":
		return tree_sitter.NewLanguage(tree_sitter_go.Language()), nil
	case "typescript":
		return tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()), nil
	case "tsx":
		return tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()), nil
	case "javascript", "jsx":
		return tree_sitter.NewLanguage(tree_sitter_javascript.Language()), nil
	default:
		return nil, fmt.Errorf("unsupported language %q", language)
	}
}

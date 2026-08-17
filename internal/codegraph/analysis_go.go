package codegraph

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func analyzeGoTree(root *tree_sitter.Node, moduleName *string, fileIndex, moduleIndex int, b *graphBuilder) {
	if root == nil {
		return
	}

	for i := uint(0); i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		switch child.Kind() {
		case "package_clause":
			if name := firstNamedText(child, b.content, "package_identifier", "identifier"); name != "" {
				*moduleName = name
				b.setModuleName(name)
			}
		case "import_declaration":
			parseGoImports(fileIndex, child, b)
		case "type_declaration":
			parseGoTypeDeclaration(child, *moduleName, fileIndex, moduleIndex, b)
		case "function_declaration", "func_declaration":
			parseGoFunction(child, *moduleName, fileIndex, moduleIndex, b)
		case "method_declaration":
			parseGoMethod(child, *moduleName, fileIndex, moduleIndex, b)
		}
	}
}

func parseGoImports(fileIndex int, node *tree_sitter.Node, b *graphBuilder) {
	for _, spec := range goImportSpecs(node) {
		path := goImportPath(spec, b.content)
		if path == "" {
			continue
		}

		b.addEdge(fileIndex, EdgeImports, path, lineFromNode(spec), spec)
	}
}

func goImportSpecs(node *tree_sitter.Node) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "import_spec" {
		return []*tree_sitter.Node{node}
	}

	result := make([]*tree_sitter.Node, 0)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		result = append(result, goImportSpecs(node.NamedChild(i))...)
	}

	return result
}

func goImportPath(node *tree_sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	if spec := firstNamed(node, "interpreted_string_literal", "string", "raw_string_literal"); spec != nil {
		return strings.Trim(nodeText(spec, content), "\"'`")
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Kind() == "interpreted_string_literal" || child.Kind() == "string" || child.Kind() == "raw_string_literal" {
			return strings.Trim(nodeText(child, content), "\"'`")
		}
	}
	return ""
}

func parseGoTypeDeclaration(node *tree_sitter.Node, moduleName string, fileIndex, moduleIndex int, b *graphBuilder) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		spec := node.NamedChild(i)
		if spec == nil || spec.Kind() != "type_spec" {
			continue
		}

		name := firstNamedText(spec, b.content, "type_identifier", "identifier")
		if name == "" {
			continue
		}

		kind := NodeType
		if firstNamed(spec, "interface_type") != nil {
			kind = NodeInterface
		}

		qname := combineQName(moduleName, name)
		idx := b.addNode(Node{
			Kind:          kind,
			Path:          b.path,
			Language:      b.language,
			Name:          name,
			QualifiedName: qname,
			Signature:     signatureFromNode(spec, b.content, "field_declaration_list", "interface_type", "struct_type"),
			LineStart:     lineFromNode(spec),
			LineEnd:       lineToNode(spec),
			ContentHash:   b.contentHash,
		})
		b.addEdge(fileIndex, EdgeContains, qname, lineFromNode(spec), spec)
		b.addEdge(moduleIndex, EdgeContains, qname, lineFromNode(spec), spec)
		collectCallsAndReferences(idx, spec, spec, b)
	}
}

func parseGoFunction(node *tree_sitter.Node, moduleName string, fileIndex, moduleIndex int, b *graphBuilder) {
	name := firstNamedText(node, b.content, "identifier")
	if name == "" {
		return
	}

	qname := combineQName(moduleName, name)
	idx := b.addNode(Node{
		Kind:          NodeFunction,
		Path:          b.path,
		Language:      b.language,
		Name:          name,
		QualifiedName: qname,
		Signature:     signatureFromNode(node, b.content, "block"),
		LineStart:     lineFromNode(node),
		LineEnd:       lineToNode(node),
		ContentHash:   b.contentHash,
	})
	b.addEdge(fileIndex, EdgeContains, qname, lineFromNode(node), node)
	b.addEdge(moduleIndex, EdgeContains, qname, lineFromNode(node), node)
	collectCallsAndReferences(idx, node, node, b)
}

func parseGoMethod(node *tree_sitter.Node, moduleName string, fileIndex, moduleIndex int, b *graphBuilder) {
	receiver := goMethodReceiverType(node, b.content)
	name := firstNamedText(node, b.content, "field_identifier", "identifier")
	if receiver == "" || name == "" {
		return
	}

	qname := combineQName(moduleName, receiver, name)
	idx := b.addNode(Node{
		Kind:          NodeMethod,
		Path:          b.path,
		Language:      b.language,
		Name:          name,
		QualifiedName: qname,
		Signature:     signatureFromNode(node, b.content, "block"),
		LineStart:     lineFromNode(node),
		LineEnd:       lineToNode(node),
		ContentHash:   b.contentHash,
	})
	b.addEdge(fileIndex, EdgeContains, qname, lineFromNode(node), node)
	b.addEdge(moduleIndex, EdgeContains, qname, lineFromNode(node), node)
	if receiverIdx := b.qNameIndex(combineQName(moduleName, receiver)); receiverIdx >= 0 {
		b.addEdge(receiverIdx, EdgeContains, qname, lineFromNode(node), node)
	}
	collectCallsAndReferences(idx, node, node, b)
}

func goMethodReceiverType(node *tree_sitter.Node, content []byte) string {
	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}
	ids := collectIdentifiers(receiver, content)
	if len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
}

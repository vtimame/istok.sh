package codegraph

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func analyzeJSishTree(root *tree_sitter.Node, moduleName string, fileIndex, moduleIndex int, b *graphBuilder) {
	var walk func(node *tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}

		switch node.Kind() {
		case "import_statement":
			path := nodeImportPath(node, b.content)
			if path != "" {
				b.addEdge(fileIndex, EdgeImports, path, lineFromNode(node), node)
			}
		case "interface_declaration":
			parseInterfaceDeclaration(node, moduleName, fileIndex, moduleIndex, b)
		case "class_declaration":
			parseClassDeclaration(node, moduleName, fileIndex, moduleIndex, b)
		case "function_declaration":
			parseFunctionDeclaration(node, moduleName, fileIndex, moduleIndex, b)
		case "export_statement", "export_default_statement":
			for i := uint(0); i < node.NamedChildCount(); i++ {
				walk(node.NamedChild(i))
			}
		default:
			for i := uint(0); i < node.NamedChildCount(); i++ {
				walk(node.NamedChild(i))
			}
		}
	}

	walk(root)
}

func nodeImportPath(node *tree_sitter.Node, content []byte) string {
	if pathNode := firstNamed(node, "string", "interpreted_string_literal", "raw_string_literal"); pathNode != nil {
		return strings.Trim(nodeText(pathNode, content), "\"'`")
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Kind() == "string" || child.Kind() == "interpreted_string_literal" || child.Kind() == "raw_string_literal" {
			return strings.Trim(nodeText(child, content), "\"'`")
		}
	}
	return ""
}

func parseInterfaceDeclaration(node *tree_sitter.Node, moduleName string, fileIndex, moduleIndex int, b *graphBuilder) {
	name := firstNamedText(node, b.content, "type_identifier", "identifier")
	if name == "" {
		return
	}

	qname := combineQName(moduleName, name)
	idx := b.addNode(Node{
		Kind:          NodeInterface,
		Path:          b.path,
		Language:      b.language,
		Name:          name,
		QualifiedName: qname,
		Signature:     signatureFromNode(node, b.content, "interface_body"),
		LineStart:     lineFromNode(node),
		LineEnd:       lineToNode(node),
		ContentHash:   b.contentHash,
	})
	b.addEdge(fileIndex, EdgeContains, qname, lineFromNode(node), node)
	b.addEdge(moduleIndex, EdgeContains, qname, lineFromNode(node), node)
	walkHeritage(idx, node, b)
}

func parseClassDeclaration(node *tree_sitter.Node, moduleName string, fileIndex, moduleIndex int, b *graphBuilder) {
	name := firstNamedText(node, b.content, "type_identifier", "identifier")
	if name == "" {
		return
	}

	qname := combineQName(moduleName, name)
	classIndex := b.addNode(Node{
		Kind:          NodeType,
		Path:          b.path,
		Language:      b.language,
		Name:          name,
		QualifiedName: qname,
		Signature:     signatureFromNode(node, b.content, "class_body"),
		LineStart:     lineFromNode(node),
		LineEnd:       lineToNode(node),
		ContentHash:   b.contentHash,
	})
	b.addEdge(fileIndex, EdgeContains, qname, lineFromNode(node), node)
	b.addEdge(moduleIndex, EdgeContains, qname, lineFromNode(node), node)
	walkHeritage(classIndex, node, b)

	body := firstNamed(node, "class_body")
	if body == nil {
		return
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		member := body.NamedChild(i)
		if member == nil || !isClassMethod(member.Kind()) {
			continue
		}
		methodName := methodNameFromNode(member, b.content)
		if methodName == "" {
			continue
		}
		methodQName := combineQName(qname, methodName)
		methodIndex := b.addNode(Node{
			Kind:          NodeMethod,
			Path:          b.path,
			Language:      b.language,
			Name:          methodName,
			QualifiedName: methodQName,
			Signature:     signatureFromNode(member, b.content, "statement_block", "function_body"),
			LineStart:     lineFromNode(member),
			LineEnd:       lineToNode(member),
			ContentHash:   b.contentHash,
		})
		b.addEdge(classIndex, EdgeContains, methodQName, lineFromNode(member), member)
		collectCallsAndReferences(methodIndex, member, member, b)
	}
}

func parseFunctionDeclaration(node *tree_sitter.Node, moduleName string, fileIndex, moduleIndex int, b *graphBuilder) {
	name := firstNamedText(node, b.content, "identifier")
	if name == "" {
		return
	}

	qname := combineQName(moduleName, name)
	nodeIndex := b.addNode(Node{
		Kind:          NodeFunction,
		Path:          b.path,
		Language:      b.language,
		Name:          name,
		QualifiedName: qname,
		Signature:     signatureFromNode(node, b.content, "statement_block", "function_body", "block"),
		LineStart:     lineFromNode(node),
		LineEnd:       lineToNode(node),
		ContentHash:   b.contentHash,
	})
	b.addEdge(fileIndex, EdgeContains, qname, lineFromNode(node), node)
	b.addEdge(moduleIndex, EdgeContains, qname, lineFromNode(node), node)
	collectCallsAndReferences(nodeIndex, node, node, b)
}

func walkHeritage(source int, node *tree_sitter.Node, b *graphBuilder) {
	if node == nil || source < 0 {
		return
	}

	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "extends_clause":
			for _, target := range collectIdentifiers(child, b.content) {
				b.addEdge(source, EdgeInherits, target, lineFromNode(child), child)
			}
		case "implements_clause":
			for _, target := range collectIdentifiers(child, b.content) {
				b.addEdge(source, EdgeImplements, target, lineFromNode(child), child)
			}
		default:
			walkHeritage(source, child, b)
		}
	}
}

func isClassMethod(nodeType string) bool {
	switch nodeType {
	case "method_definition", "public_method_definition", "private_method_definition", "get_method_definition", "set_method_definition":
		return true
	default:
		return false
	}
}

func methodNameFromNode(node *tree_sitter.Node, content []byte) string {
	for _, kind := range []string{"property_identifier", "field_identifier", "private_property_identifier", "identifier"} {
		if child := firstNamed(node, kind); child != nil {
			if value := strings.TrimSpace(nodeText(child, content)); value != "" {
				return value
			}
		}
	}
	return ""
}

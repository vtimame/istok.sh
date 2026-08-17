package codegraph

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func deriveModuleName(path, language string, root *tree_sitter.Node, content []byte) string {
	if language == "go" {
		for i := uint(0); i < root.NamedChildCount(); i++ {
			child := root.NamedChild(i)
			if child == nil {
				continue
			}
			if child.Kind() == "package_clause" {
				if name := firstNamedText(child, content, "package_identifier", "identifier"); name != "" {
					return name
				}
			}
		}
	}

	base := filepath.Base(path)
	if base == "" {
		return "module"
	}
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" {
		return "module"
	}
	return base
}

func contentLineCount(content []byte) int {
	if len(content) == 0 {
		return 1
	}
	count := 1
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	return count
}

func lineFromNode(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.StartPosition().Row) + 1
}

func lineToNode(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.EndPosition().Row) + 1
}

func firstErrorLine(root *tree_sitter.Node) int {
	if root == nil {
		return 1
	}

	stack := []*tree_sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.IsError() {
			return lineFromNode(n)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child != nil {
				stack = append(stack, child)
			}
		}
	}
	return lineFromNode(root)
}

func nodeText(node *tree_sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	if len(content) == 0 {
		return strings.TrimSpace(node.Utf8Text(nil))
	}
	return strings.TrimSpace(node.Utf8Text(content))
}

func firstNamedText(node *tree_sitter.Node, content []byte, kinds ...string) string {
	child := firstNamed(node, kinds...)
	if child == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(child, content))
}

func firstNamed(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				return child
			}
		}
	}
	return nil
}

func signatureFromNode(node *tree_sitter.Node, content []byte, stopKinds ...string) string {
	start := int(node.StartByte())
	end := int(node.EndByte())
	if start < 0 || end < 0 || start > end || end > len(content) {
		return ""
	}
	stop := make(map[string]struct{}, len(stopKinds))
	for _, kind := range stopKinds {
		stop[kind] = struct{}{}
	}

	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if _, ok := stop[child.Kind()]; !ok {
			continue
		}
		childStart := int(child.StartByte())
		if childStart > start && childStart < end {
			end = childStart
		}
	}
	if end <= start || end > len(content) {
		return ""
	}
	return strings.TrimSpace(string(content[start:end]))
}

func collectCallsAndReferences(source int, node, parent *tree_sitter.Node, b *graphBuilder) {
	if source < 0 || node == nil {
		return
	}

	switch node.Kind() {
	case "call_expression", "call":
		target := callTargetFromNode(node, b.content)
		if target != "" {
			b.addEdge(source, EdgeCalls, target, lineFromNode(node), node)
		}
	case "new_expression":
		constructor := node.ChildByFieldName("constructor")
		target := strings.TrimSpace(nodeText(constructor, b.content))
		if target != "" {
			b.addEdge(source, EdgeCalls, target, lineFromNode(node), node)
		}
	}

	if isIdentifierNode(node.Kind()) && shouldKeepReference(parent, node.Kind(), source) {
		target := strings.TrimSpace(nodeText(node, b.content))
		if target != "" {
			b.addEdge(source, EdgeReferences, target, lineFromNode(node), node)
		}
	}

	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		collectCallsAndReferences(source, child, node, b)
	}
}

func callTargetFromNode(node *tree_sitter.Node, content []byte) string {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		t := child.Kind()
		if t == "arguments" || t == "argument_list" || t == "type_arguments" {
			continue
		}
		target := strings.TrimSpace(nodeText(child, content))
		if target != "" {
			return target
		}
	}
	return ""
}

func shouldKeepReference(parent *tree_sitter.Node, nodeType string, source int) bool {
	if source < 0 {
		return false
	}
	if parent == nil {
		return true
	}

	switch parent.Kind() {
	case "package_clause", "import_spec", "import_statement", "import_clause", "type_spec", "type_parameters", "parameter", "field_declaration", "method_spec", "func_type":
		return false
	case "package_identifier", "field_identifier", "property_identifier", "private_property_identifier":
		return false
	}

	if nodeType == "this" || nodeType == "super" {
		return false
	}
	return true
}

func isIdentifierNode(nodeType string) bool {
	switch nodeType {
	case "identifier", "type_identifier", "field_identifier", "property_identifier", "private_property_identifier":
		return true
	default:
		return false
	}
}

func collectIdentifiers(node *tree_sitter.Node, content []byte) []string {
	result := make([]string, 0)
	collectIdentifiersRec(node, content, &result)
	return result
}

func collectIdentifiersRec(node *tree_sitter.Node, content []byte, result *[]string) {
	if node == nil {
		return
	}
	if isIdentifierNode(node.Kind()) {
		if value := strings.TrimSpace(nodeText(node, content)); value != "" {
			*result = append(*result, value)
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil {
			collectIdentifiersRec(child, content, result)
		}
	}
}

func combineQName(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, ".")
}

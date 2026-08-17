package codegraph

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type edgeDraft struct {
	source int
	kind   EdgeKind
	target string
	line   int
	path   string
}

type nodeDraft struct {
	node   Node
	rawIdx int
}

type graphBuilder struct {
	path         string
	language     string
	contentHash  string
	content      []byte
	nodes        []nodeDraft
	edges        []edgeDraft
	edgeKeys     map[string]struct{}
	qnameToIndex map[string]int
	byQName      map[string][]string
	byName       map[string][]string
}

func newGraphBuilder(path, language, contentHash string, content []byte) *graphBuilder {
	return &graphBuilder{
		path:         path,
		language:     language,
		contentHash:  contentHash,
		content:      content,
		nodes:        make([]nodeDraft, 0),
		edges:        make([]edgeDraft, 0),
		edgeKeys:     make(map[string]struct{}),
		qnameToIndex: make(map[string]int),
		byQName:      make(map[string][]string),
		byName:       make(map[string][]string),
	}
}

func (b *graphBuilder) setModuleName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	for i := range b.nodes {
		n := &b.nodes[i].node
		if n.Kind != NodeModule {
			continue
		}
		n.Name = name
		n.QualifiedName = name
		if n.QualifiedName != "" {
			b.qnameToIndex[n.QualifiedName] = b.nodes[i].rawIdx
		}
	}
}

func (b *graphBuilder) addNode(node Node) int {
	node.Path = b.path
	node.Language = b.language
	node.ContentHash = b.contentHash

	idx := len(b.nodes)
	b.nodes = append(b.nodes, nodeDraft{node: node, rawIdx: idx})
	if node.QualifiedName != "" {
		if _, exists := b.qnameToIndex[node.QualifiedName]; !exists {
			b.qnameToIndex[node.QualifiedName] = idx
		}
	}
	return idx
}

func (b *graphBuilder) qNameIndex(qname string) int {
	if idx, ok := b.qnameToIndex[qname]; ok {
		return idx
	}
	return -1
}

func (b *graphBuilder) addEdge(source int, kind EdgeKind, target string, line int, node *tree_sitter.Node) {
	target = strings.TrimSpace(target)
	if source < 0 || target == "" {
		return
	}
	if line <= 0 {
		line = lineFromNode(node)
	}
	if line <= 0 {
		line = 1
	}

	key := strings.Join([]string{strconv.Itoa(source), string(kind), target, strconv.Itoa(line), b.path}, "|")
	if _, ok := b.edgeKeys[key]; ok {
		return
	}

	b.edgeKeys[key] = struct{}{}
	b.edges = append(b.edges, edgeDraft{
		source: source,
		kind:   kind,
		target: target,
		line:   line,
		path:   b.path,
	})
}

func (b *graphBuilder) assignNodeIDs() map[int]string {
	indices := make([]int, len(b.nodes))
	for i := range b.nodes {
		indices[i] = i
	}

	sort.Slice(indices, func(i, j int) bool {
		a := b.nodes[indices[i]].node
		bnode := b.nodes[indices[j]].node
		if a.Kind != bnode.Kind {
			return a.Kind < bnode.Kind
		}
		if a.QualifiedName != bnode.QualifiedName {
			return a.QualifiedName < bnode.QualifiedName
		}
		if a.Signature != bnode.Signature {
			return a.Signature < bnode.Signature
		}
		if a.LineStart != bnode.LineStart {
			return a.LineStart < bnode.LineStart
		}
		if a.LineEnd != bnode.LineEnd {
			return a.LineEnd < bnode.LineEnd
		}
		return b.nodes[indices[i]].rawIdx < b.nodes[indices[j]].rawIdx
	})

	seen := make(map[string]int)
	idsByIndex := make(map[int]string, len(b.nodes))
	b.byQName = make(map[string][]string)
	b.byName = make(map[string][]string)

	for _, idx := range indices {
		n := b.nodes[idx].node
		base := deterministicNodeID(n.Path, n.Kind, n.QualifiedName, n.Signature)
		seq := seen[base]
		resolved := base
		if seq > 0 {
			resolved = fmt.Sprintf("%s#%d", base, seq)
		}
		seen[base] = seq + 1

		b.nodes[idx].node.ID = resolved
		idsByIndex[idx] = resolved

		if n.QualifiedName != "" {
			b.byQName[n.QualifiedName] = append(b.byQName[n.QualifiedName], resolved)
		}
		if n.Name != "" {
			b.byName[n.Name] = append(b.byName[n.Name], resolved)
		}
		if short := shortName(n.QualifiedName); short != "" {
			b.byName[short] = append(b.byName[short], resolved)
		}
	}

	return idsByIndex
}

func (b *graphBuilder) orderedNodes() []Node {
	out := make([]Node, len(b.nodes))
	for i, draft := range b.nodes {
		out[i] = draft.node
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			if out[i].Kind == NodeFile {
				return true
			}
			if out[j].Kind == NodeFile {
				return false
			}
			if out[i].Kind == NodeModule {
				return true
			}
			if out[j].Kind == NodeModule {
				return false
			}
			return out[i].Kind < out[j].Kind
		}
		if out[i].QualifiedName != out[j].QualifiedName {
			return out[i].QualifiedName < out[j].QualifiedName
		}
		if out[i].LineStart != out[j].LineStart {
			return out[i].LineStart < out[j].LineStart
		}
		return out[i].LineEnd < out[j].LineEnd
	})

	return out
}

func (b *graphBuilder) resolveEdges(nodeIDs map[int]string) []Edge {
	edges := make([]Edge, 0, len(b.edges))
	for _, d := range b.edges {
		sourceID := nodeIDs[d.source]
		if sourceID == "" {
			continue
		}
		targetID := resolveTargetID(d.target, b.byQName, b.byName)
		edges = append(edges, Edge{
			SourceID:     sourceID,
			TargetID:     targetID,
			TargetName:   d.target,
			Kind:         d.kind,
			Provenance:   ProvenanceExtracted,
			Confidence:   1.0,
			EvidencePath: d.path,
			EvidenceLine: d.line,
		})
	}
	return edges
}

func resolveTargetID(target string, byQName map[string][]string, byName map[string][]string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if ids, ok := byQName[target]; ok && len(ids) == 1 {
		return ids[0]
	}
	if ids, ok := byName[target]; ok && len(ids) == 1 {
		return ids[0]
	}
	return ""
}

func deterministicNodeID(path string, kind NodeKind, qname string, signature string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s", ContractVersion, path, kind, qname, signature)))
	return fmt.Sprintf("%x", h[:])
}

func shortName(qname string) string {
	parts := strings.Split(qname, ".")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

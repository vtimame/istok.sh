package retrieval

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	defaultTaskSearchMaxDirect        = 30
	defaultTaskSearchMaxItems         = 10
	defaultTaskSearchReservedDirect   = 5
	defaultTaskSearchNeighborsPerSeed = 2
	defaultTaskSearchMaxGraphOnly     = 6
	defaultTaskSearchMaxTotalBytes    = 32 * 1024
	defaultTaskSearchMaxItemBytes     = 12 * 1024
)

const (
	titleWeight       = 3.0
	descriptionWeight = 2.0
	acceptanceWeight  = 2.0
	notesWeight       = 1.0
	graphDecayFactor  = 0.35
)

// TaskSearchRequest is the task-specific input to the local retrieval pipeline.
type TaskSearchRequest struct {
	Title              string
	Description        string
	AcceptanceCriteria string
	Notes              string
	Limit              int
}

// TaskSearchOptions limits deterministic retrieval work and its output budget.
type TaskSearchOptions struct {
	MaxDirectCandidates int
	MaxItems            int
	ReserveDirect       int
	MaxNeighborsPerSeed int
	MaxGraphOnly        int
	MaxTotalBytes       int
	MaxItemBytes        int
}

func DefaultTaskSearchOptions() TaskSearchOptions {
	return TaskSearchOptions{
		MaxDirectCandidates: defaultTaskSearchMaxDirect,
		MaxItems:            defaultTaskSearchMaxItems,
		ReserveDirect:       defaultTaskSearchReservedDirect,
		MaxNeighborsPerSeed: defaultTaskSearchNeighborsPerSeed,
		MaxGraphOnly:        defaultTaskSearchMaxGraphOnly,
		MaxTotalBytes:       defaultTaskSearchMaxTotalBytes,
		MaxItemBytes:        defaultTaskSearchMaxItemBytes,
	}
}

type LexicalLookup interface {
	Search(SearchRequest) ([]SearchResult, error)
	LookupByPathRange(path string, lineStart, lineEnd int) ([]SearchResult, error)
}

// GraphNode deliberately has no dependency on a graph implementation package.
type GraphNode struct {
	ID        string
	Path      string
	Symbol    string
	LineStart int
	LineEnd   int
}

type GraphPathRangeRequest struct {
	Path      string
	LineStart int
	LineEnd   int
	Limit     int
}

type NeighborsWithMetadataRequest struct {
	SourceID string
	Limit    int
}

type GraphNeighbor struct {
	Node       GraphNode
	Kind       string
	Provenance string
	Confidence float64
}

type GraphLookup interface {
	LookupNodesByPathRange(context.Context, GraphPathRangeRequest) ([]GraphNode, error)
	NeighborsWithMetadata(context.Context, NeighborsWithMetadataRequest) ([]GraphNeighbor, error)
}

// SearchTask returns an offline, deterministic seed context for one task.
func SearchTask(ctx context.Context, lexical LexicalLookup, graph GraphLookup, request TaskSearchRequest) ([]SearchResult, error) {
	return SearchTaskWithOptions(ctx, lexical, graph, request, DefaultTaskSearchOptions())
}

func SearchTaskWithOptions(ctx context.Context, lexical LexicalLookup, graph GraphLookup, request TaskSearchRequest, options TaskSearchOptions) ([]SearchResult, error) {
	if lexical == nil || graph == nil {
		return nil, fmt.Errorf("retrieval requires lexical and graph stores")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalizeOptions(&options)

	terms, weighted := taskTerms(request)
	if len(terms) == 0 {
		return nil, nil
	}
	direct, err := lexical.Search(SearchRequest{Query: strings.Join(terms, " "), Limit: options.MaxDirectCandidates, WeightedTerms: weighted})
	if err != nil {
		return nil, err
	}
	direct = normalizeDirect(direct, terms)
	sortResults(direct)
	if len(direct) == 0 {
		return nil, nil
	}

	reservedCount := min(options.ReserveDirect, len(direct))
	graphOnly, err := expandGraph(ctx, lexical, graph, direct, options)
	if err != nil {
		return nil, err
	}

	// Direct candidates always participate before graph-only candidates; the first
	// reserved direct hits are therefore impossible to displace.
	candidates := append(append([]SearchResult(nil), direct...), graphOnly...)
	return selectBudgeted(candidates, reservedCount, request.Limit, options), nil
}

func taskTerms(request TaskSearchRequest) ([]string, []WeightedSearchTerm) {
	weights := map[string]float64{}
	literals := map[string]float64{}
	add := func(value string, weight float64) {
		for _, term := range NormalizeAndExpandIdentifiers(value) {
			term = strings.ToLower(strings.TrimSpace(term))
			if term != "" {
				weights[term] += weight
			}
		}
		seenLiterals := map[string]struct{}{}
		for _, literal := range strings.Fields(value) {
			literal = normalizeExactLiteral(literal)
			if !isExactLiteral(literal) {
				continue
			}
			if _, exists := seenLiterals[literal]; exists {
				continue
			}

			seenLiterals[literal] = struct{}{}
			literals[literal] += weight
		}
	}
	add(request.Title, titleWeight)
	add(request.Description, descriptionWeight)
	add(request.AcceptanceCriteria, acceptanceWeight)
	add(request.Notes, notesWeight)

	terms := make([]string, 0, len(weights))
	for term := range weights {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	weighted := make([]WeightedSearchTerm, 0, len(terms)+len(literals))
	for _, term := range terms {
		weighted = append(weighted, WeightedSearchTerm{Value: term, Weight: weights[term]})
	}
	orderedLiterals := make([]string, 0, len(literals))
	for literal := range literals {
		orderedLiterals = append(orderedLiterals, literal)
	}
	sort.Strings(orderedLiterals)
	for _, literal := range orderedLiterals {
		weighted = append(weighted, WeightedSearchTerm{Value: literal, Weight: literals[literal], Exact: true})
		terms = append(terms, strings.ToLower(literal))
	}
	sort.Strings(terms)

	return terms, weighted
}

func normalizeExactLiteral(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "'\"[]{}<>,;:!?")

	return strings.TrimSuffix(value, ".")
}

func isExactLiteral(value string) bool {
	return strings.ContainsAny(value, "/\\") || strings.Contains(value, ".") || strings.HasSuffix(value, "()")
}

func normalizeDirect(values []SearchResult, terms []string) []SearchResult {
	seen := make(map[string]struct{}, len(values))
	result := make([]SearchResult, 0, len(values))
	for _, value := range values {
		key := resultKey(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		value.MatchedTerms = matchedTerms(value, terms)
		if value.Provenance == nil {
			value.Provenance = []string{}
		}
		value.LexicalScore = value.Score
		value.GraphScore = 0
		value.Reasons = appendUnique(value.Reasons, ReasonLexical)
		if len(value.MatchedTerms) > 0 {
			value.Reasons = appendUnique(value.Reasons, "lexical match: "+strings.Join(value.MatchedTerms, ", "))
		}
		result = append(result, value)
	}
	return result
}

func expandGraph(ctx context.Context, lexical LexicalLookup, graph GraphLookup, direct []SearchResult, options TaskSearchOptions) ([]SearchResult, error) {
	directKeys := map[string]struct{}{}
	for _, value := range direct {
		directKeys[resultKey(value)] = struct{}{}
	}
	result := make([]SearchResult, 0, options.MaxGraphOnly)
	seen := map[string]struct{}{}
	for _, seed := range direct {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(result) >= options.MaxGraphOnly {
			break
		}
		nodes, err := graph.LookupNodesByPathRange(ctx, GraphPathRangeRequest{Path: seed.Path, LineStart: seed.LineStart, LineEnd: seed.LineEnd, Limit: 30})
		if err != nil {
			return nil, err
		}
		neighborsByKey := map[string]GraphNeighbor{}
		for _, node := range nodes {
			neighbors, err := graph.NeighborsWithMetadata(ctx, NeighborsWithMetadataRequest{SourceID: node.ID, Limit: options.MaxDirectCandidates})
			if err != nil {
				return nil, err
			}
			for _, neighbor := range neighbors {
				if neighbor.Kind == "contains" {
					continue
				}
				key := neighbor.Kind + "\x00" + neighbor.Node.ID
				if existing, ok := neighborsByKey[key]; !ok || compareNeighbor(neighbor, existing) {
					neighborsByKey[key] = neighbor
				}
			}
		}

		neighbors := make([]GraphNeighbor, 0, len(neighborsByKey))
		for _, neighbor := range neighborsByKey {
			neighbors = append(neighbors, neighbor)
		}
		sort.Slice(neighbors, func(i, j int) bool { return compareNeighbor(neighbors[i], neighbors[j]) })
		if len(neighbors) > options.MaxNeighborsPerSeed {
			neighbors = neighbors[:options.MaxNeighborsPerSeed]
		}
		for _, neighbor := range neighbors {
			if len(result) >= options.MaxGraphOnly {
				break
			}
			chunks, err := lexical.LookupByPathRange(neighbor.Node.Path, neighbor.Node.LineStart, neighbor.Node.LineEnd)
			if err != nil {
				return nil, err
			}
			if len(chunks) == 0 {
				continue
			}
			chunk := bestChunk(chunks, neighbor.Node)
			key := resultKey(chunk)
			if _, directHit := directKeys[key]; directHit {
				continue
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			chunk.LexicalScore = 0
			chunk.GraphScore = seed.LexicalScore * graphDecayFactor * edgeWeight(neighbor.Kind)
			chunk.Score = chunk.GraphScore
			chunk.MatchedTerms = []string{}
			if neighbor.Provenance != "" {
				chunk.Provenance = appendUnique(chunk.Provenance, "graph:"+neighbor.Provenance)
			}
			chunk.Reasons = []string{graphReason(seed, neighbor)}
			result = append(result, chunk)
		}
	}
	return result, nil
}

func selectBudgeted(candidates []SearchResult, reservedCount, requestedLimit int, options TaskSearchOptions) []SearchResult {
	if requestedLimit > 0 && requestedLimit < options.MaxItems {
		options.MaxItems = requestedLimit
	}
	reserved := append([]SearchResult(nil), candidates[:min(reservedCount, len(candidates))]...)
	normal := append([]SearchResult(nil), candidates[min(reservedCount, len(candidates)):]...)
	sortResults(normal)
	ordered := append(reserved, normal...)
	selected := make([]SearchResult, 0, options.MaxItems)
	seen := map[string]struct{}{}
	bytes := 0
	for _, value := range ordered {
		if len(selected) == options.MaxItems || len(value.Snippet) > options.MaxItemBytes || bytes+len(value.Snippet) > options.MaxTotalBytes {
			continue
		}
		key := resultKey(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		bytes += len(value.Snippet)
		selected = append(selected, value)
	}

	// Stable final contract order, while reservation was used only for inclusion.
	sortResults(selected)

	return selected
}

func matchedTerms(value SearchResult, terms []string) []string {
	haystack := strings.ToLower(strings.Join([]string{value.Path, value.Symbol, value.Snippet}, " "))
	matched := make([]string, 0, len(terms))
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			matched = append(matched, term)
		}
	}

	return matched
}

func bestChunk(chunks []SearchResult, node GraphNode) SearchResult {
	sort.Slice(chunks, func(i, j int) bool {
		left, right := overlap(chunks[i], node), overlap(chunks[j], node)
		if left != right {
			return left > right
		}
		return resultLess(chunks[i], chunks[j])
	})

	return chunks[0]
}

func overlap(chunk SearchResult, node GraphNode) int {
	start, end := max(chunk.LineStart, node.LineStart), min(chunk.LineEnd, node.LineEnd)
	if end < start {
		return 0
	}

	return end - start + 1
}

func compareNeighbor(a, b GraphNeighbor) bool {
	if edgeWeight(a.Kind) != edgeWeight(b.Kind) {
		return edgeWeight(a.Kind) > edgeWeight(b.Kind)
	}
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	if nodeLabel(a.Node) != nodeLabel(b.Node) {
		return nodeLabel(a.Node) < nodeLabel(b.Node)
	}
	if a.Node.Path != b.Node.Path {
		return a.Node.Path < b.Node.Path
	}
	if a.Node.LineStart != b.Node.LineStart {
		return a.Node.LineStart < b.Node.LineStart
	}
	if a.Node.LineEnd != b.Node.LineEnd {
		return a.Node.LineEnd < b.Node.LineEnd
	}
	if a.Node.ID != b.Node.ID {
		return a.Node.ID < b.Node.ID
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}

	return a.Provenance < b.Provenance
}

func edgeWeight(kind string) float64 {
	switch kind {
	case "calls", "references", "inherits", "implements":
		return .22
	case "imports":
		return .12
	case "contains":
		return .05
	default:
		return .1
	}
}

func graphReason(seed SearchResult, neighbor GraphNeighbor) string {
	edge := neighbor.Kind
	if neighbor.Provenance != "" {
		edge += " [" + neighbor.Provenance + "]"
	}

	return "graph neighbor: " + seed.Path + " -> " + edge + " -> " + nodeLabel(neighbor.Node)
}

func nodeLabel(node GraphNode) string {
	if node.Symbol != "" {
		return node.Symbol
	}

	return node.Path
}

func resultKey(value SearchResult) string {
	return fmt.Sprintf("%s#%d:%d", value.Path, value.LineStart, value.LineEnd)
}

func resultLess(a, b SearchResult) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	if a.LineStart != b.LineStart {
		return a.LineStart < b.LineStart
	}
	return a.ChunkID < b.ChunkID
}

func sortResults(values []SearchResult) {
	sort.Slice(values, func(i, j int) bool { return resultLess(values[i], values[j]) })
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}

	return append(values, value)
}

func normalizeOptions(options *TaskSearchOptions) {
	defaults := DefaultTaskSearchOptions()
	if options.MaxDirectCandidates <= 0 {
		options.MaxDirectCandidates = defaults.MaxDirectCandidates
	}
	if options.MaxItems <= 0 {
		options.MaxItems = defaults.MaxItems
	}
	if options.ReserveDirect < 0 {
		options.ReserveDirect = defaults.ReserveDirect
	}
	if options.MaxNeighborsPerSeed <= 0 {
		options.MaxNeighborsPerSeed = defaults.MaxNeighborsPerSeed
	}
	if options.MaxGraphOnly <= 0 {
		options.MaxGraphOnly = defaults.MaxGraphOnly
	}
	if options.MaxTotalBytes <= 0 {
		options.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if options.MaxItemBytes <= 0 {
		options.MaxItemBytes = defaults.MaxItemBytes
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

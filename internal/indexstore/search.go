package indexstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"

	"s26.dev/istok-cli/internal/retrieval"
)

const defaultSearchLimit = 20

const (
	exactPathBoost         = 20.0
	exactSymbolBoost       = 12.0
	weightedExactPathBoost = 8.0
	weightedSymbolBoost    = 4.0
)

// Search executes deterministic lexical search.
func (s *LexicalStore) Search(request retrieval.SearchRequest) ([]retrieval.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index == nil {
		return nil, fmt.Errorf("index is closed")
	}

	searchQuery, evidence := buildQuery(request.Query, request.WeightedTerms)
	if searchQuery == nil {
		return nil, nil
	}

	limit := request.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	bleveRequest := bleve.NewSearchRequestOptions(searchQuery, limit, 0, false)
	bleveRequest.Fields = []string{
		"chunk_id",
		"path",
		"language",
		"symbol",
		"symbol_normalized",
		"line_start",
		"line_end",
		"content_hash",
		"provenance",
		"content",
		"identifiers",
	}
	bleveRequest.Sort = stableSortOrder()

	raw, err := s.index.Search(bleveRequest)
	if err != nil {
		return nil, corrupt(s.path, err)
	}

	results := make([]retrieval.SearchResult, 0, len(raw.Hits))
	for _, hit := range raw.Hits {
		result := retrieval.SearchResult{
			ContractVersion: retrieval.ContractVersion,
			ProjectID:       s.projectID,
			EpochID:         s.epochID,
			IndexRevision:   s.revision,
			ChunkID:         asString(hit.Fields["chunk_id"]),
			Path:            asString(hit.Fields["path"]),
			Language:        asString(hit.Fields["language"]),
			Symbol:          asString(hit.Fields["symbol"]),
			LineStart:       asInt(hit.Fields["line_start"]),
			LineEnd:         asInt(hit.Fields["line_end"]),
			ContentHash:     asString(hit.Fields["content_hash"]),
			Snippet:         asString(hit.Fields["content"]),
			Score:           hit.Score,
			LexicalScore:    hit.Score,
			MatchedTerms:    []string{},
			Provenance:      resultProvenance(hit.Fields["provenance"]),
		}
		result.MatchedTerms = buildMatchedTerms(
			result.Path,
			asString(hit.Fields["symbol_normalized"]),
			result.Snippet,
			hit.Fields["identifiers"],
			evidence,
		)
		result.Reasons = buildReasons(
			result.Path,
			asString(hit.Fields["symbol_normalized"]),
			hit.Fields["identifiers"],
			evidence,
		)
		if len(result.Reasons) == 0 {
			result.Reasons = []string{retrieval.ReasonLexical}
		}

		results = append(results, result)
	}

	return results, nil
}

// LookupByPathRange returns chunks that overlap the requested range in a specific path.
func (s *LexicalStore) LookupByPathRange(path string, lineStart int, lineEnd int) ([]retrieval.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = retrieval.NormalizePath(path)
	if path == "" {
		return nil, fmt.Errorf("invalid path")
	}
	if lineStart <= 0 || lineEnd <= 0 {
		return nil, fmt.Errorf("invalid line range %d..%d", lineStart, lineEnd)
	}
	if lineEnd < lineStart {
		lineStart, lineEnd = lineEnd, lineStart
	}

	if s.index == nil {
		return nil, fmt.Errorf("index is closed")
	}

	pathQuery := query.NewTermQuery(path)
	pathQuery.SetField("path")

	lineStartQuery := query.NewNumericRangeQuery(nil, toFloat64(lineEnd))
	lineStartQuery.SetField("line_start")
	lineEndQuery := query.NewNumericRangeQuery(toFloat64(lineStart), nil)
	lineEndQuery.SetField("line_end")

	searchQuery := query.NewConjunctionQuery([]query.Query{
		pathQuery,
		lineStartQuery,
		lineEndQuery,
	})

	bleveRequest := bleve.NewSearchRequestOptions(searchQuery, defaultSearchLimit, 0, false)
	bleveRequest.Fields = []string{
		"chunk_id",
		"path",
		"language",
		"symbol",
		"symbol_normalized",
		"line_start",
		"line_end",
		"content_hash",
		"provenance",
		"content",
	}
	bleveRequest.Sort = stableSortOrderByPathRange()

	raw, err := s.index.Search(bleveRequest)
	if err != nil {
		return nil, corrupt(s.path, err)
	}

	results := make([]retrieval.SearchResult, 0, len(raw.Hits))
	for _, hit := range raw.Hits {
		result := retrieval.SearchResult{
			ContractVersion: retrieval.ContractVersion,
			ProjectID:       s.projectID,
			EpochID:         s.epochID,
			IndexRevision:   s.revision,
			ChunkID:         asString(hit.Fields["chunk_id"]),
			Path:            asString(hit.Fields["path"]),
			Language:        asString(hit.Fields["language"]),
			Symbol:          asString(hit.Fields["symbol"]),
			LineStart:       asInt(hit.Fields["line_start"]),
			LineEnd:         asInt(hit.Fields["line_end"]),
			ContentHash:     asString(hit.Fields["content_hash"]),
			Snippet:         asString(hit.Fields["content"]),
			Score:           hit.Score,
			LexicalScore:    hit.Score,
			GraphScore:      0,
			MatchedTerms:    []string{},
			Provenance:      resultProvenance(hit.Fields["provenance"]),
		}
		results = append(results, result)
	}

	return results, nil
}

func toFloat64(value int) *float64 {
	floatValue := float64(value)
	return &floatValue
}

func resultProvenance(value interface{}) []string {
	provenance := asString(value)
	if provenance == "" {
		return []string{}
	}

	return []string{provenance}
}

type queryEvidence struct {
	tokens       map[string]struct{}
	exactPaths   map[string]float64
	exactSymbols map[string]float64
}

func buildQuery(raw string, weightedTerms []retrieval.WeightedSearchTerm) (query.Query, queryEvidence) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, queryEvidence{}
	}

	evidence := queryEvidence{
		tokens:       make(map[string]struct{}),
		exactPaths:   make(map[string]float64),
		exactSymbols: make(map[string]float64),
	}
	if len(weightedTerms) == 0 {
		addExactEvidence(&evidence, raw, 1)
	}
	weights := make(map[string]float64, len(weightedTerms))
	for _, term := range weightedTerms {
		value := strings.ToLower(strings.TrimSpace(term.Value))
		if value != "" && term.Weight > weights[value] {
			weights[value] = term.Weight
		}
		if term.Exact {
			addExactEvidence(&evidence, term.Value, term.Weight)
		}
	}
	tokens := normalizedQueryTokens(raw)
	parts := make([]query.Query, 0, len(tokens)*2+len(evidence.exactPaths)+len(evidence.exactSymbols))
	pathBoost := exactPathBoost
	symbolBoost := exactSymbolBoost
	if len(weightedTerms) > 0 {
		pathBoost = weightedExactPathBoost
		symbolBoost = weightedSymbolBoost
	}

	for _, token := range tokens {
		evidence.tokens[token] = struct{}{}
		boost := weights[token]
		if boost <= 0 {
			boost = 1
		}

		identifierQuery := query.NewTermQuery(token)
		identifierQuery.SetField("identifiers")
		identifierQuery.SetBoost(6 * boost)
		parts = append(parts, identifierQuery)

		contentQuery := query.NewMatchQuery(token)
		contentQuery.SetField("content")
		contentQuery.SetBoost(2 * boost)
		parts = append(parts, contentQuery)
	}

	for _, symbol := range sortedWeightKeys(evidence.exactSymbols) {
		symbolQuery := query.NewTermQuery(symbol)
		symbolQuery.SetField("symbol_normalized")
		symbolQuery.SetBoost(symbolBoost * evidence.exactSymbols[symbol])
		parts = append(parts, symbolQuery)
	}
	for _, path := range sortedWeightKeys(evidence.exactPaths) {
		pathQuery := query.NewTermQuery(path)
		pathQuery.SetField("path")
		pathQuery.SetBoost(pathBoost * evidence.exactPaths[path])
		parts = append(parts, pathQuery)
	}

	if len(parts) == 0 {
		return nil, evidence
	}

	return query.NewDisjunctionQuery(parts), evidence
}

func addExactEvidence(evidence *queryEvidence, raw string, weight float64) {
	if weight <= 0 {
		weight = 1
	}
	if path := discoverPathQuery(raw); path != "" {
		evidence.exactPaths[path] = maxWeight(evidence.exactPaths[path], weight)
	}
	if symbol := discoverSymbolQuery(raw); symbol != "" {
		evidence.exactSymbols[symbol] = maxWeight(evidence.exactSymbols[symbol], weight)
	}
}

func maxWeight(current, candidate float64) float64 {
	if candidate > current {
		return candidate
	}

	return current
}

func sortedWeightKeys(values map[string]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func normalizedQueryTokens(raw string) []string {
	variants := retrieval.NormalizeAndExpandIdentifiers(raw)
	seen := make(map[string]struct{}, len(variants))
	tokens := make([]string, 0, len(variants))

	for _, variant := range variants {
		token := strings.ToLower(variant)
		if token == "" {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}

		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}

	return tokens
}

func discoverPathQuery(raw string) string {
	if strings.ContainsAny(raw, `/\\`) {
		return retrieval.NormalizePath(raw)
	}
	for _, extension := range []string{".go", ".ts", ".tsx", ".js", ".jsx"} {
		if strings.HasSuffix(strings.ToLower(raw), extension) {
			return retrieval.NormalizePath(raw)
		}
	}

	return ""
}

func discoverSymbolQuery(raw string) string {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "()")
	if strings.ContainsAny(raw, " \t\r\n/\\") {
		return ""
	}

	return strings.ToLower(raw)
}

func stableSortOrder() search.SortOrder {
	return search.SortOrder{
		&search.SortScore{Desc: true},
		&search.SortField{Field: "path", Type: search.SortFieldAsString},
		&search.SortField{Field: "line_start", Type: search.SortFieldAsNumber},
		&search.SortDocID{},
	}
}

func stableSortOrderByPathRange() search.SortOrder {
	return search.SortOrder{
		&search.SortField{Field: "path", Type: search.SortFieldAsString},
		&search.SortField{Field: "line_start", Type: search.SortFieldAsNumber},
		&search.SortField{Field: "chunk_id", Type: search.SortFieldAsString},
	}
}

func buildReasons(path, symbol string, identifiers interface{}, evidence queryEvidence) []string {
	reasons := make([]string, 0, 3)

	if _, ok := evidence.exactPaths[retrieval.NormalizePath(path)]; ok {
		reasons = append(reasons, retrieval.ReasonExactPath)
	}
	if _, ok := evidence.exactSymbols[symbol]; ok {
		reasons = append(reasons, retrieval.ReasonExactSymbol)
	}
	if identifiersOverlap(identifiers, evidence.tokens) {
		reasons = append(reasons, retrieval.ReasonIdentifier)
	}

	return reasons
}

func buildMatchedTerms(path, symbol, content string, identifiers interface{}, evidence queryEvidence) []string {
	values, _ := toStringSlice(identifiers)
	haystack := strings.ToLower(strings.Join(append([]string{path, symbol, content}, values...), " "))
	matched := make(map[string]struct{}, len(evidence.tokens)+2)
	for token := range evidence.tokens {
		if strings.Contains(haystack, token) {
			matched[token] = struct{}{}
		}
	}

	normalizedPath := retrieval.NormalizePath(path)
	if _, ok := evidence.exactPaths[normalizedPath]; ok {
		matched[normalizedPath] = struct{}{}
	}
	if _, ok := evidence.exactSymbols[symbol]; ok {
		matched[symbol] = struct{}{}
	}

	terms := make([]string, 0, len(matched))
	for term := range matched {
		terms = append(terms, term)
	}
	sort.Strings(terms)

	return terms
}

func identifiersOverlap(value interface{}, requested map[string]struct{}) bool {
	identifiers, err := toStringSlice(value)
	if err != nil {
		return false
	}

	for _, identifier := range identifiers {
		if _, exists := requested[strings.ToLower(identifier)]; exists {
			return true
		}
	}

	return false
}

func asString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		values, _ := toStringSlice(value)
		if len(values) > 0 {
			return values[0]
		}
	}

	return ""
}

func asInt(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case []byte:
		parsed, _ := strconv.Atoi(string(typed))
		return parsed
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func toStringSlice(value interface{}) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return strings.Fields(typed), nil
	case []string:
		return typed, nil
	case []interface{}:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}

		return values, nil
	case []byte:
		var decoded []string
		if err := json.Unmarshal(typed, &decoded); err == nil {
			return decoded, nil
		}

		return strings.Fields(string(typed)), nil
	default:
		return nil, fmt.Errorf("unsupported stored field type %T", value)
	}
}

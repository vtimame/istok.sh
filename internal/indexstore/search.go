package indexstore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"

	"s26.dev/istok-cli/internal/retrieval"
)

const defaultSearchLimit = 20

// Search executes deterministic lexical search.
func (s *LexicalStore) Search(request retrieval.SearchRequest) ([]retrieval.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index == nil {
		return nil, fmt.Errorf("index is closed")
	}

	searchQuery, evidence := buildQuery(request.Query)
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
			Provenance:      asString(hit.Fields["provenance"]),
		}
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

type queryEvidence struct {
	tokens      map[string]struct{}
	exactPath   string
	exactSymbol string
}

func buildQuery(raw string) (query.Query, queryEvidence) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, queryEvidence{}
	}

	evidence := queryEvidence{
		tokens:      make(map[string]struct{}),
		exactPath:   discoverPathQuery(raw),
		exactSymbol: discoverSymbolQuery(raw),
	}
	tokens := normalizedQueryTokens(raw)
	parts := make([]query.Query, 0, len(tokens)*2+2)

	for _, token := range tokens {
		evidence.tokens[token] = struct{}{}

		identifierQuery := query.NewTermQuery(token)
		identifierQuery.SetField("identifiers")
		identifierQuery.SetBoost(6)
		parts = append(parts, identifierQuery)

		contentQuery := query.NewMatchQuery(token)
		contentQuery.SetField("content")
		contentQuery.SetBoost(2)
		parts = append(parts, contentQuery)
	}

	if evidence.exactSymbol != "" {
		symbolQuery := query.NewTermQuery(evidence.exactSymbol)
		symbolQuery.SetField("symbol_normalized")
		symbolQuery.SetBoost(12)
		parts = append(parts, symbolQuery)
	}
	if evidence.exactPath != "" {
		pathQuery := query.NewTermQuery(evidence.exactPath)
		pathQuery.SetField("path")
		pathQuery.SetBoost(20)
		parts = append(parts, pathQuery)
	}

	if len(parts) == 0 {
		return nil, evidence
	}

	return query.NewDisjunctionQuery(parts), evidence
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

func buildReasons(path, symbol string, identifiers interface{}, evidence queryEvidence) []string {
	reasons := make([]string, 0, 3)

	if evidence.exactPath != "" && retrieval.NormalizePath(path) == evidence.exactPath {
		reasons = append(reasons, retrieval.ReasonExactPath)
	}
	if evidence.exactSymbol != "" && symbol == evidence.exactSymbol {
		reasons = append(reasons, retrieval.ReasonExactSymbol)
	}
	if identifiersOverlap(identifiers, evidence.tokens) {
		reasons = append(reasons, retrieval.ReasonIdentifier)
	}

	return reasons
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

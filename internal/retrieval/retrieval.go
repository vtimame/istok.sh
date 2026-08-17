package retrieval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const ContractVersion = "istok.retrieval.v1"

// Chunk describes transport-independent indexed file chunk metadata.
type Chunk struct {
	ID                 string   `json:"id"`
	Path               string   `json:"path"`
	Language           string   `json:"language"`
	Content            string   `json:"content"`
	Snippet            string   `json:"snippet"`
	Identifiers        []string `json:"identifiers"`
	Symbol             string   `json:"symbol"`
	SymbolNormalized   string   `json:"-"`
	LineStart          int      `json:"line_start"`
	LineEnd            int      `json:"line_end"`
	ContentHash        string   `json:"content_hash"`
	Provenance         string   `json:"provenance"`
	ChunkDiscriminator string   `json:"-"`
}

// Document is an alias kept for compatibility with legacy callers.
type Document = Chunk

// SearchRequest carries one lexical search request.
type SearchRequest struct {
	Query         string
	Limit         int
	WeightedTerms []WeightedSearchTerm
}

// WeightedSearchTerm adds token-level influence in lexical query composition.
type WeightedSearchTerm struct {
	Value  string
	Weight float64
	Exact  bool
}

// SearchResult is the transport-independent contract returned from retrieval store.
type SearchResult struct {
	ContractVersion string   `json:"contract_version"`
	ProjectID       string   `json:"project_id"`
	EpochID         string   `json:"epoch_id"`
	IndexRevision   int64    `json:"index_revision"`
	ChunkID         string   `json:"chunk_id"`
	Path            string   `json:"path"`
	Language        string   `json:"language"`
	Symbol          string   `json:"symbol"`
	LineStart       int      `json:"line_start"`
	LineEnd         int      `json:"line_end"`
	ContentHash     string   `json:"content_hash"`
	Snippet         string   `json:"snippet"`
	Score           float64  `json:"score"`
	LexicalScore    float64  `json:"lexical_score"`
	GraphScore      float64  `json:"graph_score"`
	MatchedTerms    []string `json:"matched_terms"`
	Provenance      []string `json:"provenance"`
	Reasons         []string `json:"reasons"`
}

// ApplyRequest describes delta operations for one index mutation.
type ApplyRequest struct {
	ProjectID     string
	EpochID       string
	IndexRevision int64
	Add           []Document
	DeletePaths   []string
}

// Store applies and searches retrieval documents.
type Store interface {
	Apply(request ApplyRequest) error
	Search(request SearchRequest) ([]SearchResult, error)
	Close() error
}

const (
	ReasonExactPath   = "exact_path"
	ReasonExactSymbol = "exact_symbol"
	ReasonIdentifier  = "identifier_match"
	ReasonLexical     = "lexical_match"
)

var tokenRE = regexp.MustCompile(`[\pL\pN_]+`)

// DeterministicChunkID returns stable chunk ID from path, content hash and range.
func DeterministicChunkID(path string, contentHash string, lineStart int, lineEnd int, discriminator ...string) string {
	raw := fmt.Sprintf("%s|%s|%d:%d", NormalizePath(path), contentHash, lineStart, lineEnd)
	if len(discriminator) > 0 && discriminator[0] != "" {
		raw = raw + "|" + discriminator[0]
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// NormalizeAndExpandIdentifiers splits identifiers into deterministic token variants.
func NormalizeAndExpandIdentifiers(value string) []string {
	tokens := collectIdentifierTokens(value)
	if len(tokens) == 0 {
		return nil
	}

	symbols := make([]string, 0, len(tokens)*3)
	for _, token := range tokens {
		identifiers := splitIdentifier(token)
		for _, item := range identifiers {
			if item == "" {
				continue
			}
			symbols = append(symbols, item)
		}
	}

	return dedupeExact(symbols)
}

// NormalizePath normalizes file paths into stable slash form.
func NormalizePath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" || filepath.IsAbs(value) || hasWindowsVolume(value) {
		return ""
	}

	value = strings.ReplaceAll(filepath.ToSlash(value), "\\", "/")
	value = pathpkg.Clean(value)
	if value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/") {
		return ""
	}

	return strings.TrimPrefix(value, "./")
}

func hasWindowsVolume(value string) bool {
	return len(value) >= 2 && value[1] == ':' && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z'))
}

// NormalizeForSearchToken splits and lowercases words for query matching.
func NormalizeForSearchToken(value string) []string {
	tokens := collectIdentifierTokens(value)
	for i := range tokens {
		tokens[i] = strings.ToLower(tokens[i])
	}
	return tokens
}

func collectIdentifierTokens(value string) []string {
	matches := tokenRE.FindAllString(strings.TrimSpace(value), -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		id := strings.TrimSpace(match)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func splitIdentifier(value string) []string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}

	lowered := strings.ToLower(clean)
	parts := []string{clean}
	if lowered != clean {
		parts = append(parts, lowered)
	}

	for _, snakePart := range strings.Split(clean, "_") {
		snakePart = strings.TrimSpace(snakePart)
		if snakePart == "" {
			continue
		}
		parts = append(parts, splitCamelParts(snakePart)...)
	}

	if len(parts) == 0 {
		return nil
	}

	return dedupeExact(parts)
}

func splitCamelParts(value string) []string {
	if value == "" {
		return nil
	}

	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}

	parts := make([]string, 0, len(runes))
	boundaries := []int{0}
	for i := 1; i < len(runes); i++ {
		prev := runes[i-1]
		curr := runes[i]
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		if isCamelBoundary(prev, curr, next) {
			boundaries = append(boundaries, i)
		}
	}
	boundaries = append(boundaries, len(runes))

	for i := 0; i+1 < len(boundaries); i++ {
		start := boundaries[i]
		end := boundaries[i+1]
		if end <= start {
			continue
		}
		part := string(runes[start:end])
		if part == "" {
			continue
		}
		parts = append(parts, strings.ToLower(part))
	}

	return parts
}

func isCamelBoundary(previous, current, next rune) bool {
	if unicode.IsDigit(previous) && !unicode.IsDigit(current) {
		return true
	}
	if !unicode.IsDigit(previous) && unicode.IsDigit(current) {
		return true
	}
	if unicode.IsLower(previous) && unicode.IsUpper(current) {
		return true
	}
	if unicode.IsUpper(previous) && unicode.IsUpper(current) && unicode.IsLower(next) {
		return true
	}
	return false
}

func dedupeExact(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

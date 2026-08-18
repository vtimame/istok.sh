package indexstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/standard"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/vtimame/istok.sh/internal/retrieval"
)

const (
	keyProjectID    = "project_id"
	keyEpochID      = "epoch_id"
	keyRevision     = "index_revision"
	keyFormat       = "format"
	keyPathRegistry = "path_registry:"
)

// lexicalIndexMapping creates an explicit Bleve mapping with static fields.
func lexicalIndexMapping() mapping.IndexMapping {
	idx := bleve.NewIndexMapping()
	idx.DefaultAnalyzer = standard.Name
	idx.DefaultMapping = mapping.NewDocumentStaticMapping()
	idx.DefaultMapping.Dynamic = false
	idx.IndexDynamic = false
	idx.StoreDynamic = false
	idx.DocValuesDynamic = false

	idx.DefaultMapping.AddFieldMappingsAt("chunk_id", staticKeywordField())
	idx.DefaultMapping.AddFieldMappingsAt("path", staticKeywordField())
	idx.DefaultMapping.AddFieldMappingsAt("language", staticKeywordField())
	idx.DefaultMapping.AddFieldMappingsAt("content", staticTextField())
	idx.DefaultMapping.AddFieldMappingsAt("identifiers", staticKeywordField())
	idx.DefaultMapping.AddFieldMappingsAt("symbol", staticKeywordField())
	idx.DefaultMapping.AddFieldMappingsAt("symbol_normalized", staticKeywordField())
	idx.DefaultMapping.AddFieldMappingsAt("line_start", staticNumericField())
	idx.DefaultMapping.AddFieldMappingsAt("line_end", staticNumericField())
	idx.DefaultMapping.AddFieldMappingsAt("content_hash", staticKeywordField())
	idx.DefaultMapping.AddFieldMappingsAt("provenance", staticKeywordField())

	return idx
}

func staticTextField() *mapping.FieldMapping {
	field := bleve.NewTextFieldMapping()
	field.Name = ""
	field.Store = true
	field.Index = true
	field.DocValues = false
	field.Analyzer = standard.Name
	return field
}

func staticKeywordField() *mapping.FieldMapping {
	field := bleve.NewKeywordFieldMapping()
	field.Name = ""
	field.Store = true
	field.Index = true
	field.DocValues = true
	field.Analyzer = keyword.Name
	return field
}

func staticNumericField() *mapping.FieldMapping {
	field := bleve.NewNumericFieldMapping()
	field.Name = ""
	field.Store = true
	field.Index = true
	field.DocValues = true
	return field
}

func pathRegistryKey(path string) string {
	sum := sha256.Sum256([]byte(path))
	return keyPathRegistry + hex.EncodeToString(sum[:])
}

type pathRegistry struct {
	Path     string   `json:"path"`
	ChunkIDs []string `json:"chunk_ids"`
}

type indexDocument struct {
	ChunkID          string   `json:"chunk_id"`
	Path             string   `json:"path"`
	Language         string   `json:"language"`
	Content          string   `json:"content"`
	Identifiers      []string `json:"identifiers"`
	Symbol           string   `json:"symbol"`
	SymbolNormalized string   `json:"symbol_normalized"`
	LineStart        int      `json:"line_start"`
	LineEnd          int      `json:"line_end"`
	ContentHash      string   `json:"content_hash"`
	Provenance       string   `json:"provenance"`
}

func newIndexDocument(document retrieval.Document) indexDocument {
	return indexDocument{
		ChunkID:          document.ID,
		Path:             document.Path,
		Language:         document.Language,
		Content:          document.Content,
		Identifiers:      document.Identifiers,
		Symbol:           document.Symbol,
		SymbolNormalized: strings.ToLower(strings.TrimSpace(document.Symbol)),
		LineStart:        document.LineStart,
		LineEnd:          document.LineEnd,
		ContentHash:      document.ContentHash,
		Provenance:       document.Provenance,
	}
}

func encodePathRegistry(reg pathRegistry) ([]byte, error) {
	return json.Marshal(reg)
}

func decodePathRegistry(data []byte) (pathRegistry, error) {
	if len(data) == 0 {
		return pathRegistry{}, nil
	}

	var reg pathRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return pathRegistry{}, err
	}
	return reg, nil
}

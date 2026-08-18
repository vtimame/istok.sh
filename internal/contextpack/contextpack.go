// Package contextpack combines explicit project context with local task retrieval.
package contextpack

import (
	"encoding/hex"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	projectcontext "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

const SchemaVersion = "3"

type Package struct {
	SchemaVersion string                              `json:"schema_version"`
	ProjectID     string                              `json:"project_id"`
	GeneratedAt   time.Time                           `json:"generated_at"`
	Records       []projectcontext.ContextPackageItem `json:"records"`
	Retrieval     []Item                              `json:"retrieval"`
	Metadata      Metadata                            `json:"metadata"`
}

type BuildOptions struct {
	ContextLimit            int
	WithoutRetrieval        bool
	RetrievalOverrideReason string
}

type Metadata struct {
	WithoutRetrieval bool   `json:"without_retrieval"`
	OverrideReason   string `json:"override_reason,omitempty"`
}

type Item struct {
	ItemID          string   `json:"item_id"`
	ContractVersion string   `json:"contract_version"`
	ChunkID         string   `json:"chunk_id"`
	Kind            string   `json:"kind"`
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
	Visibility      string   `json:"visibility"`
}

func (v Metadata) Validate() error {
	normalizedReason := strings.TrimSpace(v.OverrideReason)
	hasReason := normalizedReason != ""
	if v.WithoutRetrieval != hasReason {
		return projectcontext.NewError(projectcontext.CodeInvalid, "without_retrieval and override_reason must be set together")
	}
	if v.OverrideReason != normalizedReason {
		return projectcontext.NewError(projectcontext.CodeInvalid, "override_reason must be normalized")
	}

	return nil
}

func (v Item) Validate() error {
	itemID, err := uuid.Parse(v.ItemID)
	if err != nil || itemID.Version() != 7 || itemID.String() != v.ItemID {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item id must be a canonical UUIDv7")
	}
	if v.ContractVersion != retrieval.ContractVersion || strings.TrimSpace(v.ChunkID) == "" || strings.TrimSpace(v.Path) == "" || strings.TrimSpace(v.Language) == "" {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item identity is required")
	}
	if normalizedPath := retrieval.NormalizePath(v.Path); normalizedPath == "" || normalizedPath != v.Path {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item path must be normalized and project-relative")
	}
	if v.Kind != "file_chunk" && v.Kind != "symbol" {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item kind is invalid")
	}
	if v.LineStart < 1 || v.LineEnd < v.LineStart || len(v.ContentHash) != 64 {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item range or content hash is invalid")
	}
	if _, err := hex.DecodeString(v.ContentHash); err != nil {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item content hash must be a 64-character hex string")
	}
	if strings.TrimSpace(v.Snippet) == "" || v.Visibility != "local_only" || math.IsNaN(v.Score) || math.IsInf(v.Score, 0) || math.IsNaN(v.LexicalScore) || math.IsInf(v.LexicalScore, 0) || math.IsNaN(v.GraphScore) || math.IsInf(v.GraphScore, 0) {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item content, visibility, or score is invalid")
	}
	if v.MatchedTerms == nil || v.Provenance == nil || v.Reasons == nil {
		return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item metadata must be JSON arrays")
	}
	for _, values := range [][]string{v.MatchedTerms, v.Provenance, v.Reasons} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return projectcontext.NewError(projectcontext.CodeInvalid, "retrieval item metadata must not contain blank values")
			}
		}
	}

	return nil
}

func (v Package) Validate() error {
	projectID, err := uuid.Parse(v.ProjectID)
	if v.SchemaVersion != SchemaVersion || err != nil || projectID.Version() != 7 || projectID.String() != v.ProjectID || v.GeneratedAt.IsZero() {
		return projectcontext.NewError(projectcontext.CodeInvalid, "context package is invalid")
	}
	if err := v.Metadata.Validate(); err != nil {
		return err
	}
	if v.Metadata.WithoutRetrieval && len(v.Retrieval) != 0 {
		return projectcontext.NewError(projectcontext.CodeInvalid, "without_retrieval package must not contain retrieval items")
	}
	for _, item := range v.Retrieval {
		if err := item.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func FromSearchResult(itemID string, value retrieval.SearchResult) Item {
	return Item{ItemID: itemID, ContractVersion: value.ContractVersion, ChunkID: value.ChunkID, Kind: "file_chunk", Path: value.Path, Language: value.Language, Symbol: value.Symbol, LineStart: value.LineStart, LineEnd: value.LineEnd, ContentHash: value.ContentHash, Snippet: value.Snippet, Score: value.Score, LexicalScore: value.LexicalScore, GraphScore: value.GraphScore, MatchedTerms: nonNil(value.MatchedTerms), Provenance: nonNil(value.Provenance), Reasons: nonNil(value.Reasons), Visibility: "local_only"}
}

func nonNil(values []string) []string {
	return append([]string{}, values...)
}

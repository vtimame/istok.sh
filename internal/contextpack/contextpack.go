// Package contextpack combines explicit project context with local task retrieval.
package contextpack

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	projectcontext "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

const SchemaVersion = "5"

type Package struct {
	SchemaVersion string                              `json:"schema_version"`
	ProjectID     string                              `json:"project_id"`
	GeneratedAt   time.Time                           `json:"generated_at"`
	Records       []projectcontext.ContextPackageItem `json:"records"`
	Retrieval     []Item                              `json:"retrieval"`
	Knowledge     []knowledge.BriefingItem            `json:"knowledge_catalog"`
	Metadata      Metadata                            `json:"metadata"`
}

type BuildOptions struct {
	ContextLimit            int
	ExplicitContextIDs      []string
	LegacyAllContext        bool
	ContextOverrideReason   string
	WithoutRetrieval        bool
	RetrievalOverrideReason string
}

type Metadata struct {
	WithoutRetrieval      bool                            `json:"without_retrieval"`
	OverrideReason        string                          `json:"override_reason,omitempty"`
	ContextOverrideReason string                          `json:"context_override_reason,omitempty"`
	Assembly              projectcontext.AssemblyMetadata `json:"assembly"`
	Knowledge             knowledge.BriefingMetadata      `json:"knowledge"`
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
	contextReason := strings.TrimSpace(v.ContextOverrideReason)
	if v.ContextOverrideReason != contextReason {
		return projectcontext.NewError(projectcontext.CodeInvalid, "context_override_reason must be normalized")
	}
	if err := v.Assembly.Validate(contextReason != ""); err != nil {
		return err
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
	if v.Metadata.Assembly.SelectedCount != len(v.Records) {
		return projectcontext.NewError(projectcontext.CodeInvalid, "context assembly selected count does not match records")
	}
	emptyKnowledgeMetadata := len(v.Knowledge) == 0 && v.Metadata.Knowledge == (knowledge.BriefingMetadata{})
	usedKnowledgeBytes := 0
	seenKnowledge := make(map[string]bool, len(v.Knowledge))
	for _, item := range v.Knowledge {
		if !projectcontext.IsUUIDv7(item.ID) || seenKnowledge[item.ID] || item.Revision < 1 || !item.Kind.Valid() || strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Summary) == "" || len(item.Title) > knowledge.MaxTitleBytes || len(item.Summary) > knowledge.MaxSummaryBytes || len(item.ContentHash) != 64 {
			return projectcontext.NewError(projectcontext.CodeInvalid, "knowledge briefing item is invalid")
		}
		seenKnowledge[item.ID] = true
		if _, err := hex.DecodeString(item.ContentHash); err != nil {
			return projectcontext.NewError(projectcontext.CodeInvalid, "knowledge briefing hash is invalid")
		}
		encoded, _ := json.Marshal(item)
		usedKnowledgeBytes += len(encoded)
	}
	knowledgeMetadata := v.Metadata.Knowledge
	if !emptyKnowledgeMetadata && (knowledgeMetadata.SelectedCount != len(v.Knowledge) || knowledgeMetadata.CandidateCount < knowledgeMetadata.SelectedCount || knowledgeMetadata.SelectedCount > knowledge.BriefingLimit || knowledgeMetadata.BudgetBytes != knowledge.BriefingBudgetBytes || knowledgeMetadata.UsedBytes != usedKnowledgeBytes || knowledgeMetadata.UsedBytes > knowledgeMetadata.BudgetBytes || knowledgeMetadata.Truncated != (knowledgeMetadata.CandidateCount > knowledgeMetadata.SelectedCount)) {
		return projectcontext.NewError(projectcontext.CodeInvalid, "knowledge briefing metadata is invalid")
	}
	for _, record := range v.Records {
		if !record.Delivery.Valid() || math.IsNaN(record.Score) || math.IsInf(record.Score, 0) {
			return projectcontext.NewError(projectcontext.CodeInvalid, "context record selection metadata is invalid")
		}
		if record.Lane != projectcontext.LaneAlways && record.Lane != projectcontext.LaneExplicit && record.Lane != projectcontext.LaneRanked {
			return projectcontext.NewError(projectcontext.CodeInvalid, "context record inclusion lane is invalid")
		}
		if (record.Lane == projectcontext.LaneAlways && record.Delivery != projectcontext.DeliveryAlways) || (record.Lane == projectcontext.LaneRanked && record.Delivery != projectcontext.DeliveryRanked) {
			return projectcontext.NewError(projectcontext.CodeInvalid, "context record delivery does not match its inclusion lane")
		}
		if record.MatchedTerms == nil || record.Reasons == nil {
			return projectcontext.NewError(projectcontext.CodeInvalid, "context record selection metadata must be arrays")
		}
		for _, values := range [][]string{record.MatchedTerms, record.Reasons} {
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					return projectcontext.NewError(projectcontext.CodeInvalid, "context record selection metadata must not contain blank values")
				}
			}
		}
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

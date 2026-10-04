package run

import (
	"encoding/hex"
	"math"
	"strings"
	"time"

	projectcontext "github.com/vtimame/istok.sh/internal/context"
	contextpack "github.com/vtimame/istok.sh/internal/contextpack"
	"github.com/vtimame/istok.sh/internal/knowledge"
)

const (
	ContextSnapshotSchemaVersionV1 = "1"
	ContextSnapshotSchemaVersionV2 = "2"
	ContextSnapshotSchemaVersionV3 = "3"
	ContextSnapshotSchemaVersionV4 = "4"
	ContextSnapshotSchemaVersion   = "5"
)

type ContextSnapshot struct {
	ID            string                           `json:"id"`
	SchemaVersion string                           `json:"schema_version"`
	ProjectID     string                           `json:"project_id"`
	GeneratedAt   time.Time                        `json:"generated_at"`
	CreatedAt     time.Time                        `json:"created_at"`
	Records       []ContextSnapshotItem            `json:"records"`
	Retrieval     []ContextSnapshotRetrievalItem   `json:"retrieval"`
	Knowledge     []knowledge.BriefingItem         `json:"knowledge_catalog"`
	Metadata      ContextSnapshotRetrievalMetadata `json:"metadata"`
}

type ContextSnapshotRetrievalMetadata struct {
	WithoutRetrieval      bool                            `json:"without_retrieval"`
	OverrideReason        string                          `json:"override_reason,omitempty"`
	ContextOverrideReason string                          `json:"context_override_reason,omitempty"`
	Assembly              projectcontext.AssemblyMetadata `json:"assembly,omitempty"`
	Knowledge             knowledge.BriefingMetadata      `json:"knowledge,omitempty"`
}

type ContextSnapshotRetrievalItem = contextpack.Item

type ContextSnapshotItem struct {
	RecordID       string                     `json:"record_id"`
	RecordRevision int64                      `json:"record_revision"`
	ContentHash    string                     `json:"content_hash"`
	Kind           projectcontext.Kind        `json:"kind"`
	Source         projectcontext.Source      `json:"source"`
	Visibility     projectcontext.Visibility  `json:"visibility"`
	Sensitivity    projectcontext.Sensitivity `json:"sensitivity"`
	Delivery       projectcontext.Delivery    `json:"delivery,omitempty"`
	Enabled        *bool                      `json:"enabled,omitempty"`
	Priority       *projectcontext.Priority   `json:"priority,omitempty"`
	Scope          *projectcontext.Scope      `json:"scope,omitempty"`
	Title          string                     `json:"title"`
	Body           string                     `json:"body"`
	Snippet        string                     `json:"snippet"`
	Tags           []string                   `json:"tags"`
	Lane           string                     `json:"lane,omitempty"`
	Score          float64                    `json:"score,omitempty"`
	MatchedTerms   []string                   `json:"matched_terms,omitempty"`
	Reasons        []string                   `json:"reasons,omitempty"`
}

func (v ContextSnapshot) Validate() error {
	if !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "context snapshot id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.ProjectID) {
		return NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if v.SchemaVersion != ContextSnapshotSchemaVersionV1 && v.SchemaVersion != ContextSnapshotSchemaVersionV2 && v.SchemaVersion != ContextSnapshotSchemaVersionV3 && v.SchemaVersion != ContextSnapshotSchemaVersionV4 && v.SchemaVersion != ContextSnapshotSchemaVersion {
		return NewError(CodeInvalid, "invalid context snapshot schema version")
	}
	if v.GeneratedAt.IsZero() {
		return NewError(CodeInvalid, "generated at is required")
	}
	for _, value := range v.Records {
		if err := value.validate(v.SchemaVersion); err != nil {
			return err
		}
	}
	if v.SchemaVersion == ContextSnapshotSchemaVersionV1 {
		if len(v.Retrieval) != 0 || v.Metadata.WithoutRetrieval || v.Metadata.OverrideReason != "" {
			return NewError(CodeInvalid, "v1 context snapshot cannot contain retrieval")
		}
		return nil
	}
	if v.SchemaVersion == ContextSnapshotSchemaVersion {
		if err := (contextpack.Metadata{WithoutRetrieval: v.Metadata.WithoutRetrieval, OverrideReason: v.Metadata.OverrideReason, ContextOverrideReason: v.Metadata.ContextOverrideReason, Assembly: v.Metadata.Assembly, Knowledge: v.Metadata.Knowledge}).Validate(); err != nil {
			return err
		}
		if v.Metadata.Knowledge.SelectedCount != len(v.Knowledge) {
			return NewError(CodeInvalid, "context snapshot knowledge count does not match catalog")
		}
		if v.Metadata.Assembly.SelectedCount != len(v.Records) {
			return NewError(CodeInvalid, "context snapshot selected count does not match records")
		}
	} else {
		reason := strings.TrimSpace(v.Metadata.OverrideReason)
		if v.Metadata.WithoutRetrieval != (reason != "") || reason != v.Metadata.OverrideReason {
			return NewError(CodeInvalid, "legacy context snapshot retrieval metadata is invalid")
		}
	}
	if v.Metadata.WithoutRetrieval && len(v.Retrieval) != 0 {
		return NewError(CodeInvalid, "without_retrieval snapshot cannot contain retrieval")
	}
	for _, item := range v.Retrieval {
		if err := item.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func NewContextSnapshot(snapshotID string, value contextpack.Package, projectID string) (ContextSnapshot, error) {
	if snapshotID == "" || !IsUUIDv7(snapshotID) {
		return ContextSnapshot{}, NewError(CodeInvalid, "context snapshot id must be a canonical UUIDv7")
	}
	if projectID == "" || !IsUUIDv7(projectID) {
		return ContextSnapshot{}, NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if value.SchemaVersion != contextpack.SchemaVersion {
		return ContextSnapshot{}, NewError(CodeInvalid, "invalid context snapshot schema version")
	}
	if value.ProjectID == "" || value.ProjectID != projectID {
		return ContextSnapshot{}, NewError(CodeInvalid, "context package project id mismatch")
	}
	if value.GeneratedAt.IsZero() {
		return ContextSnapshot{}, NewError(CodeInvalid, "generated at is required")
	}

	if len(value.Knowledge) == 0 && value.Metadata.Knowledge == (knowledge.BriefingMetadata{}) {
		_, value.Metadata.Knowledge = knowledge.BuildBriefing(nil)
	}
	if err := value.Validate(); err != nil {
		return ContextSnapshot{}, err
	}

	records := make([]ContextSnapshotItem, len(value.Records))
	for i := range value.Records {
		record := value.Records[i]
		tags := make([]string, len(record.Tags))
		copy(tags, record.Tags)

		item := ContextSnapshotItem{
			RecordID:       record.RecordID,
			RecordRevision: record.RecordRevision,
			ContentHash:    record.ContentHash,
			Kind:           record.Kind,
			Source:         record.Source,
			Visibility:     record.Visibility,
			Sensitivity:    record.Sensitivity,
			Delivery:       record.Delivery,
			Enabled:        cloneBool(record.Enabled),
			Priority:       clonePriority(record.Priority),
			Scope:          cloneScope(record.Scope),
			Title:          record.Title,
			Body:           record.Body,
			Snippet:        record.Snippet,
			Tags:           tags,
			Lane:           record.Lane,
			Score:          record.Score,
			MatchedTerms:   append([]string{}, record.MatchedTerms...),
			Reasons:        append([]string{}, record.Reasons...),
		}

		if err := item.Validate(); err != nil {
			return ContextSnapshot{}, err
		}

		records[i] = item
	}
	retrieval := make([]ContextSnapshotRetrievalItem, len(value.Retrieval))
	for i := range value.Retrieval {
		item := value.Retrieval[i]
		item.MatchedTerms = append([]string{}, item.MatchedTerms...)
		item.Provenance = append([]string{}, item.Provenance...)
		item.Reasons = append([]string{}, item.Reasons...)
		retrieval[i] = item
	}
	briefing := append([]knowledge.BriefingItem{}, value.Knowledge...)

	assembly := value.Metadata.Assembly
	assembly.Warnings = append([]string{}, assembly.Warnings...)

	return ContextSnapshot{
		ID:            snapshotID,
		SchemaVersion: ContextSnapshotSchemaVersion,
		ProjectID:     projectID,
		GeneratedAt:   value.GeneratedAt,
		CreatedAt:     time.Now(),
		Records:       records,
		Retrieval:     retrieval,
		Knowledge:     briefing,
		Metadata:      ContextSnapshotRetrievalMetadata{WithoutRetrieval: value.Metadata.WithoutRetrieval, OverrideReason: value.Metadata.OverrideReason, ContextOverrideReason: value.Metadata.ContextOverrideReason, Assembly: assembly, Knowledge: value.Metadata.Knowledge},
	}, nil
}

func (v ContextSnapshotItem) Validate() error {
	return v.validate(ContextSnapshotSchemaVersion)
}

func (v ContextSnapshotItem) validate(schemaVersion string) error {
	if v.RecordID == "" || v.Title == "" {
		return NewError(CodeInvalid, "context snapshot item is invalid")
	}
	if !IsUUIDv7(v.RecordID) {
		return NewError(CodeInvalid, "context snapshot item record id must be a canonical UUIDv7")
	}
	if v.RecordRevision < 1 {
		return NewError(CodeInvalid, "context snapshot item revision must be >= 1")
	}
	if len(v.ContentHash) != 64 {
		return NewError(CodeInvalid, "context snapshot item content hash must be a 64-character hex string")
	}
	if _, err := hex.DecodeString(v.ContentHash); err != nil {
		return NewError(CodeInvalid, "context snapshot item content hash must be a 64-character hex string")
	}
	if !v.Kind.Valid() {
		return NewError(CodeInvalid, "context snapshot item kind is invalid")
	}
	if !v.Source.Valid() {
		return NewError(CodeInvalid, "context snapshot item source is invalid")
	}
	if !v.Visibility.Valid() {
		return NewError(CodeInvalid, "context snapshot item visibility is invalid")
	}
	if !v.Sensitivity.Valid() {
		return NewError(CodeInvalid, "context snapshot item sensitivity is invalid")
	}
	if (schemaVersion == ContextSnapshotSchemaVersionV3 || schemaVersion == ContextSnapshotSchemaVersionV4 || schemaVersion == ContextSnapshotSchemaVersion) && v.Kind == projectcontext.KindInstruction {
		if v.Enabled == nil || v.Priority == nil || v.Scope == nil || !v.Priority.Valid() || !v.Scope.Valid() {
			return NewError(CodeInvalid, "context snapshot instruction policy is invalid")
		}
	} else if v.Enabled != nil || v.Priority != nil || v.Scope != nil {
		return NewError(CodeInvalid, "context snapshot policy is only valid for instruction")
	}
	if schemaVersion == ContextSnapshotSchemaVersionV4 || schemaVersion == ContextSnapshotSchemaVersion {
		if !v.Delivery.Valid() || math.IsNaN(v.Score) || math.IsInf(v.Score, 0) {
			return NewError(CodeInvalid, "context snapshot item delivery is invalid")
		}
		if v.Lane != projectcontext.LaneAlways && v.Lane != projectcontext.LaneExplicit && v.Lane != projectcontext.LaneRanked {
			return NewError(CodeInvalid, "context snapshot item lane is invalid")
		}
		if v.MatchedTerms == nil || v.Reasons == nil {
			return NewError(CodeInvalid, "context snapshot item selection metadata must be arrays")
		}
		for _, values := range [][]string{v.MatchedTerms, v.Reasons} {
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					return NewError(CodeInvalid, "context snapshot item selection metadata must not contain blank values")
				}
			}
		}
	}
	if strings.TrimSpace(v.Title) == "" {
		return NewError(CodeInvalid, "context snapshot item title is required")
	}

	for _, value := range v.Tags {
		if strings.TrimSpace(value) == "" {
			return NewError(CodeInvalid, "context snapshot item tag must not be blank")
		}
	}

	return nil
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}

	result := *value
	return &result
}

func clonePriority(value *projectcontext.Priority) *projectcontext.Priority {
	if value == nil {
		return nil
	}

	result := *value
	return &result
}

func cloneScope(value *projectcontext.Scope) *projectcontext.Scope {
	if value == nil {
		return nil
	}

	result := *value
	return &result
}

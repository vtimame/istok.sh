package mcpserver

import (
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
)

const knowledgeSchemaVersion = "1"

type KnowledgeResult struct {
	SchemaVersion string          `json:"schema_version"`
	Knowledge     *knowledge.Item `json:"knowledge,omitempty"`
	Error         *ToolError      `json:"error,omitempty"`
}

type KnowledgeCatalogResult struct {
	SchemaVersion string                  `json:"schema_version"`
	Items         []knowledge.CatalogItem `json:"items"`
	Limit         int                     `json:"limit"`
	Offset        int                     `json:"offset"`
	Error         *ToolError              `json:"error,omitempty"`
}

type KnowledgeSupersedeResult struct {
	SchemaVersion string          `json:"schema_version"`
	Superseded    *knowledge.Item `json:"superseded,omitempty"`
	Replacement   *knowledge.Item `json:"replacement,omitempty"`
	Error         *ToolError      `json:"error,omitempty"`
}

type knowledgeCreateInput struct {
	Title       string                   `json:"title" jsonschema:"Knowledge title; must not be blank."`
	Summary     string                   `json:"summary" jsonschema:"Short catalog summary; must not be blank."`
	Body        string                   `json:"body,omitempty" jsonschema:"Full knowledge body, loaded only by show/read."`
	Kind        knowledge.Kind           `json:"kind,omitempty" jsonschema:"Knowledge kind: architecture, domain, decision, runbook, or note."`
	Tags        []string                 `json:"tags,omitempty" jsonschema:"Optional knowledge tags."`
	Visibility  contextmodel.Visibility  `json:"visibility,omitempty" jsonschema:"Visibility: shared or local_only."`
	Sensitivity contextmodel.Sensitivity `json:"sensitivity,omitempty" jsonschema:"Sensitivity: normal or private."`
	Provenance  []knowledge.Provenance   `json:"provenance,omitempty" jsonschema:"Structured source references."`
}

type knowledgeUpdateInput struct {
	KnowledgeID      string                    `json:"knowledge_id" jsonschema:"Canonical UUIDv7 knowledge item identifier."`
	ExpectedRevision int64                     `json:"expected_revision" jsonschema:"Positive current revision required for compare-and-swap."`
	Title            *string                   `json:"title,omitempty"`
	Summary          *string                   `json:"summary,omitempty"`
	Body             *string                   `json:"body,omitempty"`
	Kind             *knowledge.Kind           `json:"kind,omitempty"`
	Tags             *[]string                 `json:"tags,omitempty"`
	Visibility       *contextmodel.Visibility  `json:"visibility,omitempty"`
	Sensitivity      *contextmodel.Sensitivity `json:"sensitivity,omitempty"`
	Provenance       *[]knowledge.Provenance   `json:"provenance,omitempty"`
}

type knowledgeCatalogInput struct {
	Kinds         []knowledge.Kind           `json:"kinds,omitempty"`
	Statuses      []knowledge.Status         `json:"statuses,omitempty"`
	Visibilities  []contextmodel.Visibility  `json:"visibilities,omitempty"`
	Sensitivities []contextmodel.Sensitivity `json:"sensitivities,omitempty"`
	Limit         int                        `json:"limit,omitempty" jsonschema:"Maximum entries; defaults to 50 and cannot exceed 200."`
	Offset        int                        `json:"offset,omitempty" jsonschema:"Pagination offset."`
}

type knowledgeSearchInput struct {
	Query  string `json:"query" jsonschema:"Knowledge search query."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum entries; defaults to 50 and cannot exceed 200."`
	Offset int    `json:"offset,omitempty" jsonschema:"Pagination offset."`
}

type knowledgeShowInput struct {
	KnowledgeID string `json:"knowledge_id" jsonschema:"Canonical UUIDv7 knowledge item identifier."`
}

type knowledgeLifecycleInput struct {
	KnowledgeID      string `json:"knowledge_id" jsonschema:"Canonical UUIDv7 knowledge item identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current revision required for compare-and-swap."`
}

type knowledgeReviewInput struct {
	KnowledgeID      string `json:"knowledge_id" jsonschema:"Canonical UUIDv7 draft knowledge item identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current revision required for compare-and-swap."`
	Note             string `json:"note" jsonschema:"Required review evidence note."`
}

type knowledgeSupersedeInput struct {
	KnowledgeID      string `json:"knowledge_id" jsonschema:"Current knowledge item identifier."`
	ReplacementID    string `json:"replacement_id" jsonschema:"Replacement knowledge item identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Current item revision required for compare-and-swap."`
}

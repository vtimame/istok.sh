package mcpserver

import (
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/contextpack"
)

const contextSchemaVersion = "1"

type ContextResult struct {
	SchemaVersion string                             `json:"schema_version"`
	Context       *contextmodel.ProjectContextRecord `json:"context,omitempty"`
	Error         *ToolError                         `json:"error,omitempty"`
}

type ContextListResult struct {
	SchemaVersion string                              `json:"schema_version"`
	Contexts      []contextmodel.ProjectContextRecord `json:"contexts"`
	Error         *ToolError                          `json:"error,omitempty"`
}

type ContextPackageResult struct {
	SchemaVersion string                       `json:"schema_version"`
	Package       *contextmodel.ContextPackage `json:"package,omitempty"`
	Error         *ToolError                   `json:"error,omitempty"`
}

type ContextPreviewResult struct {
	SchemaVersion string               `json:"schema_version"`
	Package       *contextpack.Package `json:"package,omitempty"`
	Error         *ToolError           `json:"error,omitempty"`
}

type ContextDoctorResult struct {
	SchemaVersion string                         `json:"schema_version"`
	Report        *contextmodel.DiagnosticReport `json:"report,omitempty"`
	Error         *ToolError                     `json:"error,omitempty"`
}

type contextAddInput struct {
	Title        string                   `json:"title" jsonschema:"Context title; must not be blank."`
	Body         string                   `json:"body,omitempty" jsonschema:"Optional context body."`
	Kind         contextmodel.Kind        `json:"kind,omitempty" jsonschema:"Context kind: note, decision, instruction, or constraint."`
	Tags         []string                 `json:"tags,omitempty" jsonschema:"Optional context tags."`
	Source       contextmodel.Source      `json:"source,omitempty" jsonschema:"Context source: user, agent, or import."`
	Visibility   contextmodel.Visibility  `json:"visibility,omitempty" jsonschema:"Future sync visibility: shared or local_only."`
	Sensitivity  contextmodel.Sensitivity `json:"sensitivity,omitempty" jsonschema:"Context sensitivity: normal or private."`
	Delivery     contextmodel.Delivery    `json:"delivery,omitempty" jsonschema:"Delivery mode: always, ranked, or manual."`
	Priority     *contextmodel.Priority   `json:"priority,omitempty" jsonschema:"Instruction priority."`
	Scope        *contextmodel.Scope      `json:"scope,omitempty" jsonschema:"Instruction scope."`
	ReviewAfter  string                   `json:"review_after,omitempty" jsonschema:"Optional RFC3339 review timestamp."`
	ExpiresAt    string                   `json:"expires_at,omitempty" jsonschema:"Optional RFC3339 expiry timestamp."`
	SupersededBy string                   `json:"superseded_by,omitempty" jsonschema:"Optional UUIDv7 of the active replacement record."`
}

type contextUpdateInput struct {
	ContextID        string                    `json:"context_id" jsonschema:"Canonical UUIDv7 context record identifier."`
	ExpectedRevision int64                     `json:"expected_revision" jsonschema:"Positive current context revision required for compare-and-swap."`
	Title            *string                   `json:"title,omitempty" jsonschema:"Optional replacement title; must not be blank when provided."`
	Body             *string                   `json:"body,omitempty" jsonschema:"Optional replacement body."`
	Kind             *contextmodel.Kind        `json:"kind,omitempty" jsonschema:"Optional replacement kind: note, decision, instruction, or constraint."`
	Tags             *[]string                 `json:"tags,omitempty" jsonschema:"Optional replacement tags."`
	Source           *contextmodel.Source      `json:"source,omitempty" jsonschema:"Optional replacement source: user, agent, or import."`
	Visibility       *contextmodel.Visibility  `json:"visibility,omitempty" jsonschema:"Optional replacement visibility: shared or local_only."`
	Sensitivity      *contextmodel.Sensitivity `json:"sensitivity,omitempty" jsonschema:"Optional replacement sensitivity: normal or private."`
	Delivery         *contextmodel.Delivery    `json:"delivery,omitempty" jsonschema:"Optional replacement delivery mode: always, ranked, or manual."`
	Priority         *contextmodel.Priority    `json:"priority,omitempty" jsonschema:"Optional replacement instruction priority."`
	Scope            *contextmodel.Scope       `json:"scope,omitempty" jsonschema:"Optional replacement instruction scope."`
	ReviewAfter      *string                   `json:"review_after,omitempty" jsonschema:"Optional replacement RFC3339 review timestamp; empty clears it."`
	ExpiresAt        *string                   `json:"expires_at,omitempty" jsonschema:"Optional replacement RFC3339 expiry timestamp; empty clears it."`
	SupersededBy     *string                   `json:"superseded_by,omitempty" jsonschema:"Optional replacement UUIDv7; empty clears it."`
}

type contextListInput struct {
	Kinds           []contextmodel.Kind        `json:"kinds,omitempty" jsonschema:"Optional kind filters: note, decision, instruction, constraint."`
	Sources         []contextmodel.Source      `json:"sources,omitempty" jsonschema:"Optional source filters: user, agent, import."`
	Visibilities    []contextmodel.Visibility  `json:"visibilities,omitempty" jsonschema:"Optional visibility filters: shared, local_only."`
	Sensitivities   []contextmodel.Sensitivity `json:"sensitivities,omitempty" jsonschema:"Optional sensitivity filters: normal, private."`
	IncludeDeleted  bool                       `json:"include_deleted,omitempty" jsonschema:"Include archived records."`
	IncludeDisabled bool                       `json:"include_disabled,omitempty" jsonschema:"Include disabled instructions."`
	Limit           int                        `json:"limit,omitempty" jsonschema:"Maximum records to return."`
}

type contextShowInput struct {
	ContextID      string `json:"context_id" jsonschema:"Canonical UUIDv7 context record identifier."`
	IncludeDeleted bool   `json:"include_deleted,omitempty" jsonschema:"Allow archived records."`
}

type contextSearchInput struct {
	Query           string `json:"query" jsonschema:"Search query."`
	IncludeDeleted  bool   `json:"include_deleted,omitempty" jsonschema:"Include archived records."`
	IncludeDisabled bool   `json:"include_disabled,omitempty" jsonschema:"Include disabled instructions."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum records to return."`
}

type contextPackageInput struct {
	IncludeDeleted bool `json:"include_deleted,omitempty" jsonschema:"Include archived records."`
	Limit          int  `json:"limit,omitempty" jsonschema:"Maximum records to include."`
}

type contextArchiveInput struct {
	ContextID        string `json:"context_id" jsonschema:"Canonical UUIDv7 context record identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current context revision required for compare-and-swap."`
}

type contextPreviewInput struct {
	TaskID                  string   `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	ContextLimit            int      `json:"context_limit,omitempty" jsonschema:"Maximum durable context records after relevance selection."`
	ContextIDs              []string `json:"context_ids,omitempty" jsonschema:"Explicit context record UUIDv7 values to include."`
	AllContext              bool     `json:"all_context,omitempty" jsonschema:"Use legacy unbounded durable context; requires context_override_reason."`
	ContextOverrideReason   string   `json:"context_override_reason,omitempty" jsonschema:"Audited reason required with all_context."`
	WithoutRetrieval        bool     `json:"without_retrieval,omitempty" jsonschema:"Skip repository retrieval; requires retrieval_override_reason."`
	RetrievalOverrideReason string   `json:"retrieval_override_reason,omitempty" jsonschema:"Audited reason required with without_retrieval."`
}

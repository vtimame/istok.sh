package cli

import contextmodel "github.com/vtimame/istok.sh/internal/context"

type ContextCommand struct {
	Add     ContextAddCommand     `cmd:"" help:"Add saved context to the current project."`
	List    ContextListCommand    `cmd:"" help:"List saved context in the current project."`
	Show    ContextShowCommand    `cmd:"" help:"Show all context records, or one by ID."`
	Search  ContextSearchCommand  `cmd:"" help:"Search saved context in the current project."`
	Update  ContextUpdateCommand  `cmd:"" help:"Update a saved context record."`
	Enable  ContextEnabledCommand `cmd:"" help:"Enable an instruction context record."`
	Disable ContextEnabledCommand `cmd:"" help:"Disable an instruction context record."`
	Delete  ContextDeleteCommand  `cmd:"" help:"Archive a saved context record."`
	Preview ContextPreviewCommand `cmd:"" help:"Preview bounded context assembly for a task."`
	Doctor  ContextDoctorCommand  `cmd:"" help:"Diagnose context growth and lifecycle issues."`
}

type ContextAddCommand struct {
	Title        string                   `arg:"" help:"Context title."`
	Body         string                   `name:"body" help:"Context body."`
	Kind         contextmodel.Kind        `name:"kind" enum:"note,decision,instruction,constraint" default:"note" help:"Context kind."`
	Tag          []string                 `name:"tag" help:"Context tag; can be repeated."`
	Source       contextmodel.Source      `name:"source" enum:"user,agent,import" default:"user" help:"Context source."`
	Visibility   contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" default:"shared" help:"Future sync visibility."`
	Sensitivity  contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" default:"normal" help:"Context sensitivity."`
	Delivery     *contextmodel.Delivery   `name:"delivery" enum:"always,ranked,manual" help:"Delivery mode; defaults to always for instructions and ranked otherwise."`
	Priority     *contextmodel.Priority   `name:"priority" enum:"critical,high,normal,low" help:"Instruction priority."`
	Scope        *contextmodel.Scope      `name:"scope" enum:"project" help:"Instruction scope."`
	ReviewAfter  string                   `name:"review-after" help:"RFC3339 timestamp after which the record is due for review."`
	ExpiresAt    string                   `name:"expires-at" help:"RFC3339 timestamp after which automatic assembly excludes the record."`
	SupersededBy string                   `name:"superseded-by" help:"UUIDv7 of the active record replacing this one."`
	Database     string                   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON         bool                     `name:"json" help:"Write a versioned JSON response."`
}

type ContextListCommand struct {
	Kind            []contextmodel.Kind        `name:"kind" enum:"note,decision,instruction,constraint" help:"Context kind filter; can be repeated."`
	Source          []contextmodel.Source      `name:"source" enum:"user,agent,import" help:"Context source filter; can be repeated."`
	Visibility      []contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" help:"Visibility filter; can be repeated."`
	Sensitivity     []contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" help:"Sensitivity filter; can be repeated."`
	IncludeDeleted  bool                       `name:"deleted" help:"Include archived context records."`
	IncludeDisabled bool                       `name:"include-disabled" help:"Include disabled instructions."`
	Limit           int                        `name:"limit" help:"Maximum number of records to return."`
	Database        string                     `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON            bool                       `name:"json" help:"Write a versioned JSON response."`
}

type ContextShowCommand struct {
	ID              string `arg:"" optional:"" help:"Canonical UUIDv7 context record ID; omit to show all project context."`
	IncludeDeleted  bool   `name:"deleted" help:"Include archived context records."`
	IncludeDisabled bool   `name:"include-disabled" help:"Include disabled instructions when showing all records."`
	Database        string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON            bool   `name:"json" help:"Write a versioned JSON response."`
}

type ContextSearchCommand struct {
	Query           string `arg:"" help:"Search query."`
	IncludeDeleted  bool   `name:"deleted" help:"Include archived context records."`
	IncludeDisabled bool   `name:"include-disabled" help:"Include disabled instructions."`
	Limit           int    `name:"limit" help:"Maximum number of records to return."`
	Database        string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON            bool   `name:"json" help:"Write a versioned JSON response."`
}

type ContextUpdateCommand struct {
	ID               string                    `arg:"" help:"Canonical UUIDv7 context record ID."`
	ExpectedRevision int64                     `name:"expected-revision" required:"" help:"Current record revision required for compare-and-swap."`
	Title            *string                   `name:"title" help:"Replacement title."`
	Body             *string                   `name:"body" help:"Replacement body."`
	Kind             *contextmodel.Kind        `name:"kind" enum:"note,decision,instruction,constraint" help:"Replacement context kind."`
	Tag              []string                  `name:"tag" help:"Replacement tag; can be repeated."`
	Source           *contextmodel.Source      `name:"source" enum:"user,agent,import" help:"Replacement source."`
	Visibility       *contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" help:"Replacement visibility."`
	Sensitivity      *contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" help:"Replacement sensitivity."`
	Delivery         *contextmodel.Delivery    `name:"delivery" enum:"always,ranked,manual" help:"Replacement delivery mode."`
	Priority         *contextmodel.Priority    `name:"priority" enum:"critical,high,normal,low" help:"Replacement instruction priority."`
	Scope            *contextmodel.Scope       `name:"scope" enum:"project" help:"Replacement instruction scope."`
	ReviewAfter      *string                   `name:"review-after" help:"Replacement RFC3339 review timestamp; pass an empty value to clear."`
	ExpiresAt        *string                   `name:"expires-at" help:"Replacement RFC3339 expiry timestamp; pass an empty value to clear."`
	SupersededBy     *string                   `name:"superseded-by" help:"Replacement superseding UUIDv7; pass an empty value to clear."`
	Database         string                    `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool                      `name:"json" help:"Write a versioned JSON response."`
}

type ContextDeleteCommand struct {
	ID               string `arg:"" help:"Canonical UUIDv7 context record ID."`
	ExpectedRevision int64  `name:"expected-revision" required:"" help:"Current record revision required for compare-and-swap."`
	Yes              bool   `name:"yes" help:"Archive without an interactive confirmation."`
	Database         string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool   `name:"json" help:"Write a versioned JSON response; requires --yes."`
}

type ContextEnabledCommand struct {
	ID               string `arg:"" help:"Canonical UUIDv7 instruction context record ID."`
	ExpectedRevision int64  `name:"expected-revision" required:"" help:"Current record revision required for compare-and-swap."`
	Database         string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool   `name:"json" help:"Write a versioned JSON response."`
}

type ContextPreviewCommand struct {
	Task                    int64    `arg:"" help:"Task number in the current project."`
	ContextLimit            int      `name:"context-limit" help:"Explicit durable record limit (1-64); zero auto-expands for required always context."`
	ContextID               []string `name:"context-id" help:"Explicit context record UUIDv7 to include; can be repeated."`
	AllContext              bool     `name:"all-context" help:"Use legacy unbounded durable context; requires --context-override-reason."`
	ContextOverrideReason   string   `name:"context-override-reason" help:"Audited reason required with --all-context."`
	WithoutRetrieval        bool     `name:"without-retrieval" help:"Skip repository retrieval; requires --retrieval-override-reason."`
	RetrievalOverrideReason string   `name:"retrieval-override-reason" help:"Audited reason required with --without-retrieval."`
	Database                string   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON                    bool     `name:"json" help:"Write a versioned JSON response."`
}

type ContextDoctorCommand struct {
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

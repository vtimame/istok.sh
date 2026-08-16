package cli

import contextmodel "s26.dev/istok-cli/internal/context"

type ContextCommand struct {
	Add    ContextAddCommand    `cmd:"" help:"Add saved context to the current project."`
	List   ContextListCommand   `cmd:"" help:"List saved context in the current project."`
	Show   ContextShowCommand   `cmd:"" help:"Show all context records, or one by ID."`
	Search ContextSearchCommand `cmd:"" help:"Search saved context in the current project."`
	Update ContextUpdateCommand `cmd:"" help:"Update a saved context record."`
	Delete ContextDeleteCommand `cmd:"" help:"Archive a saved context record."`
}

type ContextAddCommand struct {
	Title       string                   `arg:"" help:"Context title."`
	Body        string                   `name:"body" help:"Context body."`
	Kind        contextmodel.Kind        `name:"kind" enum:"note,decision,instruction,constraint" default:"note" help:"Context kind."`
	Tag         []string                 `name:"tag" help:"Context tag; can be repeated."`
	Source      contextmodel.Source      `name:"source" enum:"user,agent,import" default:"user" help:"Context source."`
	Visibility  contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" default:"shared" help:"Future sync visibility."`
	Sensitivity contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" default:"normal" help:"Context sensitivity."`
	Database    string                   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON        bool                     `name:"json" help:"Write a versioned JSON response."`
}

type ContextListCommand struct {
	Kind           []contextmodel.Kind        `name:"kind" enum:"note,decision,instruction,constraint" help:"Context kind filter; can be repeated."`
	Source         []contextmodel.Source      `name:"source" enum:"user,agent,import" help:"Context source filter; can be repeated."`
	Visibility     []contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" help:"Visibility filter; can be repeated."`
	Sensitivity    []contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" help:"Sensitivity filter; can be repeated."`
	IncludeDeleted bool                       `name:"deleted" help:"Include archived context records."`
	Limit          int                        `name:"limit" help:"Maximum number of records to return."`
	Database       string                     `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON           bool                       `name:"json" help:"Write a versioned JSON response."`
}

type ContextShowCommand struct {
	ID             string `arg:"" optional:"" help:"Canonical UUIDv7 context record ID; omit to show all project context."`
	IncludeDeleted bool   `name:"deleted" help:"Include archived context records."`
	Database       string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON           bool   `name:"json" help:"Write a versioned JSON response."`
}

type ContextSearchCommand struct {
	Query          string `arg:"" help:"Search query."`
	IncludeDeleted bool   `name:"deleted" help:"Include archived context records."`
	Limit          int    `name:"limit" help:"Maximum number of records to return."`
	Database       string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON           bool   `name:"json" help:"Write a versioned JSON response."`
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

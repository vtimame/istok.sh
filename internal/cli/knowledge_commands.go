package cli

import (
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
)

type KnowledgeCommand struct {
	Add       KnowledgeAddCommand       `cmd:"" help:"Create a draft knowledge item."`
	Distill   KnowledgeAddCommand       `cmd:"" help:"Create a draft knowledge item from explicit provenance sources."`
	List      KnowledgeListCommand      `cmd:"" aliases:"catalog" help:"List the bounded knowledge catalog without full bodies."`
	Search    KnowledgeSearchCommand    `cmd:"" help:"Search knowledge and return bounded catalog entries."`
	Show      KnowledgeShowCommand      `cmd:"" help:"Show one complete knowledge item."`
	Read      KnowledgeShowCommand      `cmd:"" help:"Read one complete knowledge item."`
	Update    KnowledgeUpdateCommand    `cmd:"" help:"Update draft knowledge."`
	Review    KnowledgeReviewCommand    `cmd:"" help:"Record explicit review of a draft."`
	Promote   KnowledgeLifecycleCommand `cmd:"" help:"Promote reviewed draft knowledge to current."`
	Supersede KnowledgeSupersedeCommand `cmd:"" help:"Supersede current knowledge with a replacement."`
	Export    KnowledgeExportCommand    `cmd:"" help:"Export knowledge to an explicit destination."`
}

type KnowledgeAddCommand struct {
	Title       string                   `arg:"" help:"Knowledge title."`
	Summary     string                   `name:"summary" required:"" help:"Short catalog summary."`
	Body        string                   `name:"body" help:"Knowledge body."`
	BodyFile    string                   `name:"body-file" type:"path" help:"Read the body from this explicit path."`
	Kind        knowledge.Kind           `name:"kind" enum:"architecture,domain,decision,runbook,note" default:"note" help:"Knowledge kind."`
	Tag         []string                 `name:"tag" help:"Knowledge tag; can be repeated."`
	Source      []string                 `name:"source" help:"Provenance TYPE:ID[@REVISION][#DETAIL]; can be repeated."`
	Visibility  contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" default:"shared" help:"Knowledge visibility."`
	Sensitivity contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" default:"normal" help:"Knowledge sensitivity."`
	Database    string                   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON        bool                     `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeListCommand struct {
	Kind        []knowledge.Kind           `name:"kind" enum:"architecture,domain,decision,runbook,note" help:"Kind filter; can be repeated."`
	Status      []knowledge.Status         `name:"status" enum:"draft,current,superseded" help:"Status filter; can be repeated."`
	Visibility  []contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" help:"Visibility filter; can be repeated."`
	Sensitivity []contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" help:"Sensitivity filter; can be repeated."`
	Limit       int                        `name:"limit" default:"50" help:"Maximum catalog entries (max 200)."`
	Offset      int                        `name:"offset" help:"Catalog pagination offset."`
	Database    string                     `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON        bool                       `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeSearchCommand struct {
	Query    string `arg:"" help:"Search query."`
	Limit    int    `name:"limit" default:"50" help:"Maximum catalog entries (max 200)."`
	Offset   int    `name:"offset" help:"Search pagination offset."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeShowCommand struct {
	ID       string `arg:"" help:"Canonical UUIDv7 knowledge item ID."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeUpdateCommand struct {
	ID               string                    `arg:"" help:"Canonical UUIDv7 knowledge item ID."`
	ExpectedRevision int64                     `name:"expected-revision" required:"" help:"Current revision required for compare-and-swap."`
	Title            *string                   `name:"title" help:"Replacement title."`
	Summary          *string                   `name:"summary" help:"Replacement summary."`
	Body             *string                   `name:"body" help:"Replacement body."`
	BodyFile         string                    `name:"body-file" type:"path" help:"Read the replacement body from this explicit path."`
	Kind             *knowledge.Kind           `name:"kind" enum:"architecture,domain,decision,runbook,note" help:"Replacement kind."`
	Tag              []string                  `name:"tag" help:"Replacement tags; can be repeated."`
	Source           []string                  `name:"source" help:"Replacement provenance; can be repeated."`
	Visibility       *contextmodel.Visibility  `name:"visibility" enum:"shared,local_only" help:"Replacement visibility."`
	Sensitivity      *contextmodel.Sensitivity `name:"sensitivity" enum:"normal,private" help:"Replacement sensitivity."`
	Database         string                    `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool                      `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeLifecycleCommand struct {
	ID               string `arg:"" help:"Canonical UUIDv7 knowledge item ID."`
	ExpectedRevision int64  `name:"expected-revision" required:"" help:"Current revision required for compare-and-swap."`
	Database         string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool   `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeReviewCommand struct {
	ID               string `arg:"" help:"Canonical UUIDv7 draft knowledge item ID."`
	ExpectedRevision int64  `name:"expected-revision" required:"" help:"Current revision required for compare-and-swap."`
	Note             string `name:"note" required:"" help:"Review evidence note."`
	Database         string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool   `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeSupersedeCommand struct {
	ID               string `arg:"" help:"Canonical UUIDv7 current knowledge item ID."`
	ReplacementID    string `arg:"" help:"Canonical UUIDv7 replacement knowledge item ID."`
	ExpectedRevision int64  `name:"expected-revision" required:"" help:"Current revision required for compare-and-swap."`
	Database         string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool   `name:"json" help:"Write a versioned JSON response."`
}

type KnowledgeExportCommand struct {
	ID       string `arg:"" help:"Canonical UUIDv7 knowledge item ID."`
	Output   string `name:"output" type:"path" required:"" help:"Explicit export destination; existing files are refused."`
	Format   string `name:"format" enum:"markdown,json" default:"markdown" help:"Export format."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON command response."`
}

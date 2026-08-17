package cli

import "s26.dev/istok-cli/internal/codegraph"

type IndexCommand struct {
	Status  IndexStatusCommand  `cmd:"" help:"Show local index state."`
	Rebuild IndexRebuildCommand `cmd:"" help:"Force a full index rebuild for repair."`
}

type IndexStatusCommand struct {
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type IndexRebuildCommand struct {
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type SearchCommand struct {
	Query    string `arg:"" help:"Text, path, or symbol to search for."`
	Limit    int    `name:"limit" default:"10" help:"Maximum number of results."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type GraphCommand struct {
	Symbol    GraphSymbolCommand    `cmd:"" help:"Find graph symbols by name."`
	Neighbors GraphNeighborsCommand `cmd:"" help:"Find graph neighbors of a symbol."`
	Path      GraphPathCommand      `cmd:"" help:"Find bounded paths between two symbols."`
}

type GraphSymbolCommand struct {
	Name     string `arg:"" help:"Symbol name or qualified name."`
	Limit    int    `name:"limit" default:"20" help:"Maximum number of symbols."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type GraphNeighborsCommand struct {
	Name     string               `arg:"" help:"Source symbol name or qualified name."`
	Kinds    []codegraph.EdgeKind `name:"kind" enum:"contains,imports,calls,references,inherits,implements" help:"Only include this relation kind; may be repeated."`
	Limit    int                  `name:"limit" default:"20" help:"Maximum number of neighbors."`
	Database string               `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool                 `name:"json" help:"Write a versioned JSON response."`
}

type GraphPathCommand struct {
	From     string `arg:"" help:"Source symbol name or qualified name."`
	To       string `arg:"" help:"Target symbol name or qualified name."`
	MaxDepth int    `name:"max-depth" default:"6" help:"Maximum graph traversal depth."`
	Limit    int    `name:"limit" default:"10" help:"Maximum number of paths."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

package graphstore

import (
	"database/sql"

	"s26.dev/istok-cli/internal/codegraph"
)

// Repository provides SQLite-backed access to graph facts.
type Repository struct {
	db *sql.DB
}

const (
	defaultLookupLimit   = 50
	defaultMaxDepth      = 8
	maxTraversalDepth    = 32
	defaultMaxPathResult = 32
	maxResultLimit       = 500
	maxTraversalSteps    = 10000
)

// LookupSymbolsRequest selects symbol nodes by qualified name and/or short name.
type LookupSymbolsRequest struct {
	QualifiedName string
	Name          string
	Limit         int
}

// NeighborsRequest resolves outgoing resolved neighbors from one node.
type NeighborsRequest struct {
	SourceID string
	Kinds    []codegraph.EdgeKind
	Limit    int
}

// PathRequest asks a bounded deterministic shortest-path query.
type PathRequest struct {
	From       string
	To         string
	MaxDepth   int
	MaxResults int
}

// PathsRequest is an alias for requesting multiple path candidates.
type PathsRequest = PathRequest

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

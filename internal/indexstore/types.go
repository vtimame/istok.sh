package indexstore

import (
	"github.com/vtimame/istok.sh/internal/retrieval"
)

const FormatVersion = "istok.index.v1"

// ApplyRequest is accepted by Store.Apply.
type ApplyRequest = retrieval.ApplyRequest

// Store applies indexed documents and performs lexical search.
type Store interface {
	Apply(request ApplyRequest) error
	Search(request retrieval.SearchRequest) ([]retrieval.SearchResult, error)
	LookupByPathRange(path string, lineStart, lineEnd int) ([]retrieval.SearchResult, error)
	Close() error
}

// Repository exposes additional metadata for orchestration.
type Repository interface {
	Store
	ProjectID() string
	EpochID() string
	Revision() int64
	Path() string
	DocCount() (uint64, error)
}

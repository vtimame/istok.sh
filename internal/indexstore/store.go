package indexstore

import (
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
)

var _ Store = (*LexicalStore)(nil)
var _ Repository = (*LexicalStore)(nil)

// LexicalStore is a Bleve-backed lexical retrieval store.
type LexicalStore struct {
	path      string
	projectID string
	epochID   string
	revision  int64
	index     bleve.Index
	mu        sync.Mutex
}

// Path returns the index path.
func (s *LexicalStore) Path() string { return s.path }

// ProjectID returns the project marker.
func (s *LexicalStore) ProjectID() string { return s.projectID }

// EpochID returns the generation marker.
func (s *LexicalStore) EpochID() string { return s.epochID }

// Revision returns the current index revision.
func (s *LexicalStore) Revision() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.revision
}

// DocCount returns the number of indexed chunks.
func (s *LexicalStore) DocCount() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index == nil {
		return 0, fmt.Errorf("index is closed")
	}

	count, err := s.index.DocCount()
	if err != nil {
		return 0, corrupt(s.path, err)
	}

	return count, nil
}

// Close releases underlying resources.
func (s *LexicalStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index == nil {
		return nil
	}

	index := s.index
	s.index = nil

	return index.Close()
}

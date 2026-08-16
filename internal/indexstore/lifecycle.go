package indexstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/google/uuid"
)

// Create creates a new lexical index with required metadata markers.
func Create(path, projectID, epochID string, revision int64) (*LexicalStore, error) {
	projectID = strings.TrimSpace(projectID)
	epochID = strings.TrimSpace(epochID)

	if err := validateProjectEpoch(path, projectID, epochID); err != nil {
		return nil, err
	}
	if revision < 0 {
		return nil, incompatible(path, fmt.Errorf("revision must be non-negative"))
	}
	if strings.TrimSpace(path) == "" {
		return nil, incompatible(path, fmt.Errorf("empty path"))
	}

	if _, err := os.Stat(path); err == nil {
		return nil, incompatible(path, fmt.Errorf("index path already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, corrupt(path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, corrupt(path, err)
	}

	index, err := bleve.New(path, lexicalIndexMapping())
	if err != nil {
		return nil, corrupt(path, err)
	}

	store := &LexicalStore{
		path:      path,
		projectID: projectID,
		epochID:   epochID,
		revision:  revision,
		index:     index,
	}
	batch := index.NewBatch()
	store.setMetadataInBatch(revision, batch)

	if err := index.Batch(batch); err != nil {
		_ = index.Close()
		_ = os.RemoveAll(path)

		return nil, corrupt(path, err)
	}

	return store, nil
}

// Open validates and opens an existing lexical index.
func Open(path, projectID, epochID string, revision int64) (*LexicalStore, error) {
	projectID = strings.TrimSpace(projectID)
	epochID = strings.TrimSpace(epochID)

	if err := validateProjectEpoch(path, projectID, epochID); err != nil {
		return nil, err
	}
	if revision < 0 {
		return nil, incompatible(path, fmt.Errorf("revision must be non-negative"))
	}
	if strings.TrimSpace(path) == "" {
		return nil, incompatible(path, fmt.Errorf("empty path"))
	}

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, missing(path)
		}

		return nil, corrupt(path, err)
	}

	index, err := bleve.Open(path)
	if err != nil {
		return nil, corrupt(path, err)
	}

	store := &LexicalStore{path: path, index: index}
	if err := store.reloadMetadata(); err != nil {
		_ = index.Close()

		return nil, err
	}

	if store.projectID != projectID {
		_ = index.Close()

		return nil, incompatible(path, fmt.Errorf("project id mismatch: got %q, expected %q", store.projectID, projectID))
	}
	if store.epochID != epochID {
		_ = index.Close()

		return nil, incompatible(path, fmt.Errorf("epoch id mismatch: got %q, expected %q", store.epochID, epochID))
	}
	if store.revision != revision {
		_ = index.Close()

		return nil, revisionMismatch(path, fmt.Errorf("revision mismatch: got %d, expected %d", store.revision, revision))
	}

	return store, nil
}

func (s *LexicalStore) reloadMetadata() error {
	projectID, err := s.getInternalString(keyProjectID)
	if err != nil {
		return err
	}

	epochID, err := s.getInternalString(keyEpochID)
	if err != nil {
		return err
	}

	revisionValue, err := s.getInternalString(keyRevision)
	if err != nil {
		return err
	}

	format, err := s.getInternalString(keyFormat)
	if err != nil {
		return err
	}
	if format != FormatVersion {
		return incompatible(s.path, fmt.Errorf("unsupported format %q", format))
	}

	revision, err := strconv.ParseInt(revisionValue, 10, 64)
	if err != nil || revision < 0 {
		return corrupt(s.path, fmt.Errorf("invalid revision marker %q", revisionValue))
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return corrupt(s.path, fmt.Errorf("invalid project id marker: %w", err))
	}
	if _, err := uuid.Parse(epochID); err != nil {
		return corrupt(s.path, fmt.Errorf("invalid epoch id marker: %w", err))
	}

	s.projectID = projectID
	s.epochID = epochID
	s.revision = revision

	return nil
}

func (s *LexicalStore) setMetadataInBatch(revision int64, batch *bleve.Batch) {
	batch.SetInternal([]byte(keyProjectID), []byte(s.projectID))
	batch.SetInternal([]byte(keyEpochID), []byte(s.epochID))
	batch.SetInternal([]byte(keyRevision), []byte(strconv.FormatInt(revision, 10)))
	batch.SetInternal([]byte(keyFormat), []byte(FormatVersion))
}

func (s *LexicalStore) getInternalString(key string) (string, error) {
	value, err := s.index.GetInternal([]byte(key))
	if err != nil {
		return "", corrupt(s.path, err)
	}
	if len(value) == 0 {
		return "", corrupt(s.path, fmt.Errorf("missing internal key %q", key))
	}

	return string(value), nil
}

func validateProjectEpoch(path, projectID, epochID string) error {
	if _, err := uuid.Parse(projectID); err != nil {
		return incompatible(path, fmt.Errorf("invalid project id: %w", err))
	}
	if _, err := uuid.Parse(epochID); err != nil {
		return incompatible(path, fmt.Errorf("invalid epoch id: %w", err))
	}

	return nil
}

package indexstore

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vtimame/istok.sh/internal/retrieval"
)

// Apply atomically replaces chunks for the affected paths and advances revision.
func (s *LexicalStore) Apply(request ApplyRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index == nil {
		return fmt.Errorf("index is closed")
	}
	if request.ProjectID != s.projectID {
		return incompatible(s.path, fmt.Errorf("project id mismatch: got %q, expected %q", request.ProjectID, s.projectID))
	}
	if request.EpochID != s.epochID {
		return incompatible(s.path, fmt.Errorf("epoch id mismatch: got %q, expected %q", request.EpochID, s.epochID))
	}
	if request.IndexRevision != 0 && request.IndexRevision <= s.revision {
		return revisionMismatch(s.path, fmt.Errorf("revision mismatch: current=%d requested=%d", s.revision, request.IndexRevision))
	}

	targetRevision := request.IndexRevision
	if targetRevision == 0 {
		targetRevision = s.revision + 1
	}

	deletePaths := make(map[string]struct{}, len(request.DeletePaths)+len(request.Add))
	for _, rawPath := range request.DeletePaths {
		path := retrieval.NormalizePath(rawPath)
		if path == "" {
			return incompatible(s.path, fmt.Errorf("empty delete path"))
		}

		deletePaths[path] = struct{}{}
	}

	addsByPath := make(map[string][]retrieval.Document, len(request.Add))
	seenIDs := make(map[string]struct{}, len(request.Add))
	pathHashes := make(map[string]string, len(request.Add))

	for _, document := range request.Add {
		path := retrieval.NormalizePath(document.Path)
		if path == "" {
			return incompatible(s.path, fmt.Errorf("empty document path"))
		}
		if document.ContentHash == "" {
			return incompatible(s.path, fmt.Errorf("missing content hash for path %q", path))
		}
		if document.LineStart < 1 || document.LineEnd < document.LineStart {
			return incompatible(s.path, fmt.Errorf("invalid line range for path %q", path))
		}
		if strings.TrimSpace(document.Provenance) == "" {
			return incompatible(s.path, fmt.Errorf("missing provenance for path %q", path))
		}

		expectedID := retrieval.DeterministicChunkID(path, document.ContentHash, document.LineStart, document.LineEnd, document.ChunkDiscriminator)
		if document.ID == "" {
			document.ID = expectedID
		} else if document.ID != expectedID {
			return incompatible(s.path, fmt.Errorf("chunk id mismatch for path %q", path))
		}
		if _, exists := seenIDs[document.ID]; exists {
			return incompatible(s.path, fmt.Errorf("duplicate chunk id %q", document.ID))
		}
		seenIDs[document.ID] = struct{}{}

		if previousHash, exists := pathHashes[path]; exists && previousHash != document.ContentHash {
			return incompatible(s.path, fmt.Errorf("conflicting content hash for path %q", path))
		}
		pathHashes[path] = document.ContentHash

		document.Path = path
		addsByPath[path] = append(addsByPath[path], document)
		deletePaths[path] = struct{}{}
	}

	for path := range addsByPath {
		sort.Slice(addsByPath[path], func(i, j int) bool {
			left := addsByPath[path][i]
			right := addsByPath[path][j]

			if left.LineStart != right.LineStart {
				return left.LineStart < right.LineStart
			}
			if left.LineEnd != right.LineEnd {
				return left.LineEnd < right.LineEnd
			}

			return left.ID < right.ID
		})
	}

	batch := s.index.NewBatch()
	for _, path := range sortedSetKeys(deletePaths) {
		ids, err := s.loadPathRegistry(path)
		if err != nil {
			return err
		}
		for _, id := range ids {
			batch.Delete(id)
		}
		batch.DeleteInternal([]byte(pathRegistryKey(path)))
	}

	for _, path := range sortedDocumentPaths(addsByPath) {
		documents := addsByPath[path]
		chunkIDs := make([]string, 0, len(documents))
		for _, document := range documents {
			if err := batch.Index(document.ID, newIndexDocument(document)); err != nil {
				return corrupt(s.path, err)
			}
			chunkIDs = append(chunkIDs, document.ID)
		}

		encoded, err := encodePathRegistry(pathRegistry{Path: path, ChunkIDs: chunkIDs})
		if err != nil {
			return corrupt(s.path, err)
		}
		batch.SetInternal([]byte(pathRegistryKey(path)), encoded)
	}

	s.setMetadataInBatch(targetRevision, batch)
	if err := s.index.Batch(batch); err != nil {
		return corrupt(s.path, err)
	}

	s.revision = targetRevision

	return nil
}

func (s *LexicalStore) loadPathRegistry(path string) ([]string, error) {
	data, err := s.index.GetInternal([]byte(pathRegistryKey(path)))
	if err != nil {
		return nil, corrupt(s.path, err)
	}
	if len(data) == 0 {
		return nil, nil
	}

	registry, err := decodePathRegistry(data)
	if err != nil {
		return nil, corrupt(s.path, err)
	}
	if registry.Path != path {
		return nil, corrupt(s.path, fmt.Errorf("path registry mismatch for %q", path))
	}

	seen := make(map[string]struct{}, len(registry.ChunkIDs))
	for _, id := range registry.ChunkIDs {
		if id == "" {
			return nil, corrupt(s.path, fmt.Errorf("empty chunk id in registry for %q", path))
		}
		if _, exists := seen[id]; exists {
			return nil, corrupt(s.path, fmt.Errorf("duplicate chunk id in registry for %q", path))
		}
		seen[id] = struct{}{}
	}

	return registry.ChunkIDs, nil
}

func sortedSetKeys(items map[string]struct{}) []string {
	paths := make([]string, 0, len(items))
	for path := range items {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return paths
}

func sortedDocumentPaths(items map[string][]retrieval.Document) []string {
	paths := make([]string, 0, len(items))
	for path := range items {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return paths
}

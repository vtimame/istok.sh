// Package manifest provides file manifests and deterministic diffs for indexing.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/vtimame/istok.sh/internal/indexing/discovery"
)

// Entry describes one indexed file metadata record.
type Entry struct {
	Path                string `json:"path"`
	SizeBytes           int64  `json:"size_bytes"`
	ModTimeNs           int64  `json:"mtime_ns"`
	ContentHash         string `json:"content_hash"`
	Language            string `json:"language"`
	ChunkCount          int64  `json:"chunk_count"`
	SymbolCount         int64  `json:"symbol_count"`
	LastIndexedRevision int64  `json:"last_indexed_revision"`
}

// Manifest is a set of file entries in deterministic order.
type Manifest struct {
	Files []Entry `json:"files"`
}

// Rename records one-to-one rename inference.
type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Diff stores the result of manifest comparison.
type Diff struct {
	Added     []string
	Modified  []string
	Deleted   []string
	Renamed   []Rename
	Unchanged []string
}

// Build builds a manifest and computes a deterministic diff.
func Build(previous Manifest, files []discovery.File, revision int64) (Manifest, Diff, error) {
	previousByPath := make(map[string]Entry, len(previous.Files))
	previousByHash := make(map[string][]string)
	for _, item := range previous.Files {
		previousByPath[item.Path] = item
		previousByHash[item.ContentHash] = append(previousByHash[item.ContentHash], item.Path)
	}

	seen := make(map[string]struct{})
	current := make(map[string]Entry, len(files))
	added := make(map[string]Entry)
	modified := make(map[string]Entry)
	unchanged := make(map[string]Entry)

	for _, file := range files {
		if file.Path == "" {
			continue
		}
		if _, exists := seen[file.Path]; exists {
			continue
		}
		seen[file.Path] = struct{}{}

		previousEntry, hadPrevious := previousByPath[file.Path]
		entry, err := buildEntry(previousEntry, hadPrevious, file, revision)
		if err != nil {
			return Manifest{}, Diff{}, fmt.Errorf("build entry for %q: %w", file.Path, err)
		}

		if !hadPrevious {
			added[file.Path] = entry
		} else if previousEntry.ContentHash != entry.ContentHash {
			modified[file.Path] = entry
		} else {
			unchanged[file.Path] = entry
		}
		current[file.Path] = entry
	}

	deleted := make(map[string]Entry)
	for _, item := range previous.Files {
		if _, exists := current[item.Path]; !exists {
			deleted[item.Path] = item
		}
	}

	renames := detectRenames(previousByHash, added, deleted)
	for _, rename := range renames {
		delete(added, rename.To)
		delete(deleted, rename.From)
	}

	result := Manifest{Files: make([]Entry, 0, len(current))}
	for path := range current {
		result.Files = append(result.Files, current[path])
	}
	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].Path < result.Files[j].Path
	})

	diff := Diff{
		Added:     mapKeys(added),
		Modified:  mapKeys(modified),
		Deleted:   mapKeys(deleted),
		Unchanged: mapKeys(unchanged),
		Renamed:   renames,
	}

	return result, diff, nil
}

func buildEntry(previous Entry, existed bool, file discovery.File, revision int64) (Entry, error) {
	entry := Entry{
		Path:                file.Path,
		SizeBytes:           file.SizeBytes,
		ModTimeNs:           file.ModTimeNs,
		Language:            file.Language,
		LastIndexedRevision: revision,
	}

	if file.ContentHash != "" {
		entry.ContentHash = file.ContentHash
	}

	if existed && previous.SizeBytes == file.SizeBytes && previous.ModTimeNs == file.ModTimeNs {
		if entry.ContentHash == "" {
			entry.ContentHash = previous.ContentHash
		}
		entry.ChunkCount = previous.ChunkCount
		entry.SymbolCount = previous.SymbolCount
		entry.LastIndexedRevision = previous.LastIndexedRevision
		return entry, nil
	}

	if entry.ContentHash != "" && existed && previous.ContentHash == entry.ContentHash {
		entry.ChunkCount = previous.ChunkCount
		entry.SymbolCount = previous.SymbolCount
		entry.LastIndexedRevision = previous.LastIndexedRevision
		return entry, nil
	}

	if entry.ContentHash == "" {
		hash, err := hashContent(file.AbsPath)
		if err != nil {
			return Entry{}, err
		}
		entry.ContentHash = hash
	}

	if existed && previous.ContentHash == entry.ContentHash {
		entry.ChunkCount = previous.ChunkCount
		entry.SymbolCount = previous.SymbolCount
		entry.LastIndexedRevision = previous.LastIndexedRevision
	}

	return entry, nil
}

func detectRenames(previousByHash map[string][]string, added map[string]Entry, deleted map[string]Entry) []Rename {
	addedByHash := make(map[string][]string, len(added))
	for path, item := range added {
		addedByHash[item.ContentHash] = append(addedByHash[item.ContentHash], path)
	}

	deletedByHash := make(map[string][]string, len(deleted))
	for path, item := range deleted {
		deletedByHash[item.ContentHash] = append(deletedByHash[item.ContentHash], path)
	}

	pairs := make([]Rename, 0)
	for hash := range previousByHash {
		addedPaths := addedByHash[hash]
		deletedPaths := deletedByHash[hash]
		if len(addedPaths) == 0 || len(deletedPaths) == 0 {
			continue
		}

		sort.Strings(addedPaths)
		sort.Strings(deletedPaths)

		pairCount := len(addedPaths)
		if pairCount > len(deletedPaths) {
			pairCount = len(deletedPaths)
		}

		for i := 0; i < pairCount; i++ {
			addedPath := addedPaths[i]
			deletedPath := deletedPaths[i]
			if addedPath == deletedPath {
				continue
			}
			pairs = append(pairs, Rename{From: deletedPath, To: addedPath})
			delete(added, addedPath)
			delete(deleted, deletedPath)
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].From == pairs[j].From {
			return pairs[i].To < pairs[j].To
		}
		return pairs[i].From < pairs[j].From
	})
	return pairs
}

func mapKeys(value map[string]Entry) []string {
	paths := make([]string, 0, len(value))
	for path := range value {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func hashContent(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

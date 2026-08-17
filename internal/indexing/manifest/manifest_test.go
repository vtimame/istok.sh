package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"s26.dev/istok-cli/internal/indexing/discovery"
)

func TestBuildManifestReusedHashAndDiff(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	firstPath := writeFile(t, root, "one.go", "alpha")
	secondPath := writeFile(t, root, "two.go", "beta")
	deletedPath := writeFile(t, root, "deleted.go", "gone")

	previousHashA, err := hashContent(firstPath)
	if err != nil {
		t.Fatalf("hashContent() error = %v", err)
	}
	previousHashB, err := hashContent(secondPath)
	if err != nil {
		t.Fatalf("hashContent() error = %v", err)
	}
	previousHashC, err := hashContent(deletedPath)
	if err != nil {
		t.Fatalf("hashContent() error = %v", err)
	}

	previous := Manifest{Files: []Entry{
		{
			Path:                "one.go",
			SizeBytes:           int64(len("alpha")),
			ModTimeNs:           1_000,
			ContentHash:         previousHashA,
			Language:            "go",
			ChunkCount:          2,
			SymbolCount:         3,
			LastIndexedRevision: 7,
		},
		{
			Path:                "two.go",
			SizeBytes:           int64(len("beta")),
			ModTimeNs:           2_000,
			ContentHash:         previousHashB,
			Language:            "go",
			ChunkCount:          4,
			SymbolCount:         5,
			LastIndexedRevision: 7,
		},
		{
			Path:                "deleted.go",
			SizeBytes:           int64(len("gone")),
			ModTimeNs:           3_000,
			ContentHash:         previousHashC,
			Language:            "go",
			LastIndexedRevision: 7,
		},
	}}

	currentFiles := []discovery.File{
		{
			Path:      "renamed.go",
			AbsPath:   firstPath,
			SizeBytes: int64(len("alpha")),
			ModTimeNs: 10_000,
			Language:  "go",
		},
		{
			Path:      "two.go",
			AbsPath:   writeFile(t, root, "two.go", "changed"),
			SizeBytes: int64(len("changed")),
			ModTimeNs: 11_000,
			Language:  "go",
		},
		{
			Path:      "three.go",
			AbsPath:   writeFile(t, root, "three.go", "other"),
			SizeBytes: int64(len("other")),
			ModTimeNs: 12_000,
			Language:  "go",
		},
	}

	current, diff, err := Build(previous, currentFiles, 8)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(current.Files) != 3 {
		t.Fatalf("manifest len = %d, want %d", len(current.Files), 3)
	}

	if got := findByPath(current, "renamed.go").LastIndexedRevision; got != 8 {
		t.Fatalf("renamed.go LastIndexedRevision = %d, want %d", got, 8)
	}
	if !containsStrings(diff.Renamed, "one.go", "renamed.go") {
		t.Error("expected rename one.go -> renamed.go")
	}
	if !containsStringSlice(diff.Added, "three.go") {
		t.Error("expected added file three.go")
	}
	if !containsStringSlice(diff.Modified, "two.go") {
		t.Error("expected modified two.go")
	}
	if len(diff.Deleted) != 1 || diff.Deleted[0] != "deleted.go" {
		t.Fatalf("deleted = %v, want [deleted.go]", diff.Deleted)
	}
}

func TestBuildManifestModifiedAndUnchanged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeFile(t, root, "same.txt", "content")
	previousHash, err := hashContent(path)
	if err != nil {
		t.Fatalf("hashContent() error = %v", err)
	}

	previous := Manifest{Files: []Entry{{
		Path:                "same.txt",
		SizeBytes:           int64(len("content")),
		ModTimeNs:           100,
		ContentHash:         previousHash,
		Language:            "text",
		LastIndexedRevision: 3,
	}}}

	currentFiles := []discovery.File{{
		Path:      "same.txt",
		AbsPath:   filepath.Join(root, "missing-after-stat.txt"),
		SizeBytes: int64(len("content")),
		ModTimeNs: 100,
		Language:  "text",
	}}

	_, diff, err := Build(previous, currentFiles, 4)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(diff.Modified) != 0 {
		t.Errorf("modified = %v, want empty", diff.Modified)
	}
	if len(diff.Unchanged) != 1 || diff.Unchanged[0] != "same.txt" {
		t.Fatalf("unchanged = %v, want [same.txt]", diff.Unchanged)
	}
}

func TestBuildManifestUsesProvidedHashWithoutReadingFile(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "same.txt", "content")
	previousHash, err := hashContent(path)
	if err != nil {
		t.Fatalf("hashContent() error = %v", err)
	}

	previous := Manifest{Files: []Entry{{
		Path:                "same.txt",
		SizeBytes:           int64(len("content")),
		ModTimeNs:           100,
		ContentHash:         previousHash,
		Language:            "text",
		ChunkCount:          12,
		SymbolCount:         34,
		LastIndexedRevision: 3,
	}}}

	currentFiles := []discovery.File{{
		Path:        "same.txt",
		AbsPath:     filepath.Join(root, "missing-after-stat.txt"),
		SizeBytes:   int64(len("changed")),
		ModTimeNs:   200,
		Language:    "text",
		ContentHash: previousHash,
	}}

	current, diff, err := Build(previous, currentFiles, 4)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	entry := findByPath(current, "same.txt")
	if entry.ChunkCount != 12 {
		t.Fatalf("entry.ChunkCount = %d, want 12", entry.ChunkCount)
	}
	if entry.SymbolCount != 34 {
		t.Fatalf("entry.SymbolCount = %d, want 34", entry.SymbolCount)
	}
	if entry.LastIndexedRevision != 3 {
		t.Fatalf("entry.LastIndexedRevision = %d, want 3", entry.LastIndexedRevision)
	}
	if len(diff.Unchanged) != 1 || diff.Unchanged[0] != "same.txt" {
		t.Fatalf("unchanged = %v, want [same.txt]", diff.Unchanged)
	}
}

func TestBuildManifestDuplicateContentRenamesOneToOne(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	originalA := writeFile(t, root, "first.go", "shared")
	originalB := writeFile(t, root, "second.go", "shared")
	changed := writeFile(t, root, "changed.txt", "changed")

	hashShared, err := hashContent(originalA)
	if err != nil {
		t.Fatalf("hashContent() error = %v", err)
	}

	previous := Manifest{Files: []Entry{
		{
			Path:                "first.go",
			SizeBytes:           int64(len("shared")),
			ModTimeNs:           1,
			ContentHash:         hashShared,
			Language:            "go",
			ChunkCount:          1,
			SymbolCount:         1,
			LastIndexedRevision: 1,
		},
		{
			Path:                "second.go",
			SizeBytes:           int64(len("shared")),
			ModTimeNs:           2,
			ContentHash:         hashShared,
			Language:            "go",
			ChunkCount:          1,
			SymbolCount:         1,
			LastIndexedRevision: 1,
		},
	}}

	currentFiles := []discovery.File{
		{
			Path:      "renamed-a.go",
			AbsPath:   originalA,
			SizeBytes: int64(len("shared")),
			ModTimeNs: 10,
			Language:  "go",
		},
		{
			Path:      "renamed-b.go",
			AbsPath:   originalB,
			SizeBytes: int64(len("shared")),
			ModTimeNs: 20,
			Language:  "go",
		},
		{
			Path:      "changed.txt",
			AbsPath:   changed,
			SizeBytes: int64(len("changed")),
			ModTimeNs: 30,
			Language:  "text",
		},
	}
	_, diff, err := Build(previous, currentFiles, 2)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(diff.Renamed) != 2 {
		t.Fatalf("renamed count = %d, want 2", len(diff.Renamed))
	}
	if !containsRename(diff.Renamed, "first.go", "renamed-a.go") {
		t.Fatal("missing rename first.go -> renamed-a.go")
	}
	if !containsRename(diff.Renamed, "second.go", "renamed-b.go") {
		t.Fatal("missing rename second.go -> renamed-b.go")
	}
	if !containsStringSlice(diff.Added, "changed.txt") {
		t.Fatalf("expected changed.txt in added: %v", diff.Added)
	}
	if len(diff.Deleted) != 0 {
		t.Fatalf("deleted = %v, want empty", diff.Deleted)
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file %s: %v", name, err)
	}
	return path
}

func findByPath(manifest Manifest, path string) Entry {
	for _, item := range manifest.Files {
		if item.Path == path {
			return item
		}
	}
	return Entry{}
}

func containsStringSlice(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsRename(renames []Rename, from, to string) bool {
	for _, rename := range renames {
		if rename.From == from && rename.To == to {
			return true
		}
	}
	return false
}

func containsStrings(renames []Rename, from, to string) bool {
	for _, rename := range renames {
		if rename.From == from && rename.To == to {
			return true
		}
	}
	return false
}

package sidecar

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"s26.dev/istok-cli/internal/indexing/manifest"
	"s26.dev/istok-cli/internal/indexstore"
	"s26.dev/istok-cli/internal/retrieval"
)

func TestSidecarProjectIsolatedByProjectID(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	base := t.TempDir()
	root1 := filepath.Join(base, "root1")
	root2 := filepath.Join(base, "root2")
	if err := os.MkdirAll(root1, 0o700); err != nil {
		t.Fatalf("MkdirAll root1: %v", err)
	}
	if err := os.MkdirAll(root2, 0o700); err != nil {
		t.Fatalf("MkdirAll root2: %v", err)
	}

	project1, err := Open(context.Background(), OpenConfig{
		ProjectID:     "11111111-1111-1111-1111-111111111111",
		CanonicalRoot: root1,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open(project1) error = %v", err)
	}
	defer project1.Close()

	project2, err := Open(context.Background(), OpenConfig{
		ProjectID:     "22222222-2222-2222-2222-222222222222",
		CanonicalRoot: root2,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open(project2) error = %v", err)
	}
	defer project2.Close()

	generation1, err := project1.CreateGeneration(context.Background(), 1, StateReady, nil)
	if err != nil {
		t.Fatalf("CreateGeneration(project1) error = %v", err)
	}
	defer generation1.Close()

	generation2, err := project2.CreateGeneration(context.Background(), 1, StateReady, nil)
	if err != nil {
		t.Fatalf("CreateGeneration(project2) error = %v", err)
	}
	defer generation2.Close()

	entries, err := os.ReadDir(filepath.Join(indexRoot))
	if err != nil {
		t.Fatalf("ReadDir(indexRoot) error = %v", err)
	}
	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Name()] = true
	}
	if !got["11111111-1111-1111-1111-111111111111"] || !got["22222222-2222-2222-2222-222222222222"] {
		t.Fatalf("both project directories expected, got = %v", got)
	}
}

func TestSidecarProjectIDValidation(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	root := t.TempDir()

	if _, err := Open(context.Background(), OpenConfig{
		ProjectID:     "not-a-uuid",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	}); err == nil {
		t.Fatal("expected invalid project ID error")
	}

	if err := Delete(context.Background(), indexRoot, "not-a-uuid"); err == nil {
		t.Fatal("expected invalid project ID error")
	}
}

func TestSidecarLockSerialization(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	root := t.TempDir()
	cfg := OpenConfig{
		ProjectID:     "33333333-3333-3333-3333-333333333333",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	}

	first, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}
	defer first.Close()

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if _, err := Open(timeoutCtx, cfg); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected lock timeout, got %v", err)
	}

	deleteCtx, cancelDelete := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelDelete()
	if err := Delete(deleteCtx, indexRoot, cfg.ProjectID); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected delete lock timeout, got %v", err)
	}
}

func TestSidecarCreateOpenRebindAndLifecycle(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	rootA := t.TempDir()
	rootB := t.TempDir()

	pA, err := Open(context.Background(), OpenConfig{
		ProjectID:     "44444444-4444-4444-4444-444444444444",
		CanonicalRoot: rootA,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open(pA) error = %v", err)
	}

	generation, err := pA.CreateGeneration(context.Background(), 10, StateReady, []string{"init"})
	if err != nil {
		_ = pA.Close()
		t.Fatalf("CreateGeneration error = %v", err)
	}
	if _, err := pA.Current(context.Background()); !errors.Is(err, ErrMissing) {
		_ = generation.Close()
		_ = pA.Close()
		t.Fatalf("unpublished generation must not become current, got %v", err)
	}
	if err := generation.Publish(context.Background()); err != nil {
		_ = generation.Close()
		_ = pA.Close()
		t.Fatalf("Publish error = %v", err)
	}
	if err := generation.Close(); err != nil {
		_ = pA.Close()
		t.Fatalf("generation.Close() error = %v", err)
	}

	current, err := pA.Current(context.Background())
	if err != nil {
		_ = pA.Close()
		t.Fatalf("Current error = %v", err)
	}
	state, err := current.State()
	if err != nil {
		_ = pA.Close()
		t.Fatalf("State() error = %v", err)
	}
	if state.Epoch != generation.Epoch() {
		_ = pA.Close()
		t.Fatalf("current epoch = %q, want %q", state.Epoch, generation.Epoch())
	}
	if err := current.Close(); err != nil {
		_ = pA.Close()
		t.Fatalf("current.Close() error = %v", err)
	}
	_ = pA.Close()

	pA2, err := Open(context.Background(), OpenConfig{
		ProjectID:     "44444444-4444-4444-4444-444444444444",
		CanonicalRoot: rootB,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open(pA2) error = %v", err)
	}
	defer pA2.Close()

	if _, err := pA2.Current(context.Background()); !errors.Is(err, ErrRebind) {
		t.Fatalf("expected rebind error, got %v", err)
	}

	if _, err := uuid.Parse(generation.Epoch()); err != nil {
		t.Fatalf("generation epoch not uuid: %v", err)
	}
}

func TestSidecarMissingGraphDbIsCorrupt(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	root := t.TempDir()

	project, err := Open(context.Background(), OpenConfig{
		ProjectID:     "66666666-6666-6666-6666-666666666666",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	gen, err := project.CreateGeneration(context.Background(), 1, StateReady, nil)
	if err != nil {
		_ = project.Close()
		t.Fatalf("CreateGeneration() error = %v", err)
	}
	if err := gen.Publish(context.Background()); err != nil {
		_ = gen.Close()
		_ = project.Close()
		t.Fatalf("Publish() error = %v", err)
	}
	if err := gen.Close(); err != nil {
		_ = project.Close()
		t.Fatalf("gen.Close() error = %v", err)
	}

	_ = project.Close()
	graphPath := filepath.Join(indexRoot, "66666666-6666-6666-6666-666666666666", "generations", gen.Epoch(), "graph.db")
	if err := os.Remove(graphPath); err != nil {
		t.Fatalf("remove graph.db: %v", err)
	}

	reopened, err := Open(context.Background(), OpenConfig{
		ProjectID:     "66666666-6666-6666-6666-666666666666",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() reopened error = %v", err)
	}
	defer reopened.Close()

	if _, err := reopened.Current(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected corrupt error for missing graph.db, got %v", err)
	}
}

func TestSidecarCompatibleMetadataValidation(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	root := t.TempDir()

	project, err := Open(context.Background(), OpenConfig{
		ProjectID:     "77777777-7777-7777-7777-777777777777",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	gen, err := project.CreateGeneration(context.Background(), 2, StateReady, nil)
	if err != nil {
		_ = project.Close()
		t.Fatalf("CreateGeneration() error = %v", err)
	}
	if err := gen.Publish(context.Background()); err != nil {
		_ = gen.Close()
		_ = project.Close()
		t.Fatalf("Publish() error = %v", err)
	}

	statePath := filepath.Join(indexRoot, "77777777-7777-7777-7777-777777777777", "generations", gen.Epoch(), "state.json")
	graphPath := filepath.Join(indexRoot, "77777777-7777-7777-7777-777777777777", "generations", gen.Epoch(), "graph.db")

	if err := project.Close(); err != nil {
		t.Fatalf("project.Close() error = %v", err)
	}
	if err := gen.Close(); err != nil {
		t.Fatalf("gen.Close() error = %v", err)
	}

	if err := restoreStateWith(
		t,
		statePath,
		root,
		gen.Epoch(),
		2,
		"77777777-7777-7777-7777-777777777777",
		SidecarFormat,
		"bad.schema",
	); err != nil {
		t.Fatalf("write tampered state: %v", err)
	}

	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(graphPath))
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO sidecar_meta(key,value) VALUES('graph_schema', 'bad.schema')`); err != nil {
		_ = db.Close()
		t.Fatalf("inject wrong metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := Open(context.Background(), OpenConfig{
		ProjectID:     "77777777-7777-7777-7777-777777777777",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() reopened error = %v", err)
	}
	defer reopened.Close()
	if _, err := reopened.Current(context.Background()); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected incompatible error for tampered constants, got %v", err)
	}
}

func TestSidecarDeleteAndCorruptionCases(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	root := t.TempDir()

	project, err := Open(context.Background(), OpenConfig{
		ProjectID:     "55555555-5555-5555-5555-555555555555",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	gen, err := project.CreateGeneration(context.Background(), 1, StateReady, nil)
	if err != nil {
		_ = project.Close()
		t.Fatalf("CreateGeneration() error = %v", err)
	}
	if err := gen.Publish(context.Background()); err != nil {
		_ = gen.Close()
		_ = project.Close()
		t.Fatalf("Publish() error = %v", err)
	}

	statePath := filepath.Join(indexRoot, "55555555-5555-5555-5555-555555555555", "generations", gen.Epoch(), "state.json")
	graphPath := filepath.Join(indexRoot, "55555555-5555-5555-5555-555555555555", "generations", gen.Epoch(), "graph.db")
	currentPath := filepath.Join(indexRoot, "55555555-5555-5555-5555-555555555555", "CURRENT")

	if err := gen.Close(); err != nil {
		_ = project.Close()
		t.Fatalf("gen.Close() error = %v", err)
	}
	if err := project.Close(); err != nil {
		t.Fatalf("project.Close() error = %v", err)
	}

	// malformed CURRENT
	if err := os.WriteFile(currentPath, []byte("invalid\n"), 0o600); err != nil {
		t.Fatalf("write malformed CURRENT: %v", err)
	}
	reopened, err := Open(context.Background(), OpenConfig{
		ProjectID:     "55555555-5555-5555-5555-555555555555",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() for malformed current error = %v", err)
	}
	if _, err := reopened.Current(context.Background()); !errors.Is(err, ErrCorrupt) {
		_ = reopened.Close()
		t.Fatalf("expected corrupt for malformed current, got %v", err)
	}
	_ = reopened.Close()

	if err := os.WriteFile(currentPath, []byte(gen.Epoch()+"\n"), 0o600); err != nil {
		t.Fatalf("restore CURRENT: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("bad-json"), 0o600); err != nil {
		t.Fatalf("break state json: %v", err)
	}
	reopened, err = Open(context.Background(), OpenConfig{
		ProjectID:     "55555555-5555-5555-5555-555555555555",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() reopened error = %v", err)
	}
	if _, err := reopened.Current(context.Background()); !errors.Is(err, ErrCorrupt) {
		_ = reopened.Close()
		t.Fatalf("expected corrupt for malformed state, got %v", err)
	}
	_ = reopened.Close()

	if err := restoreState(t, statePath, root, gen.Epoch(), 1); err != nil {
		t.Fatalf("restore state: %v", err)
	}
	reopened, err = Open(context.Background(), OpenConfig{
		ProjectID:     "55555555-5555-5555-5555-555555555555",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() reopened error = %v", err)
	}

	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(graphPath))
	if err != nil {
		_ = reopened.Close()
		t.Fatalf("open graph db for test: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO sidecar_meta(key,value) VALUES('sidecar_format', 'other')`); err != nil {
		_ = db.Close()
		_ = reopened.Close()
		t.Fatalf("inject wrong metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		_ = reopened.Close()
		t.Fatalf("close db: %v", err)
	}

	if _, err := reopened.Current(context.Background()); !errors.Is(err, ErrIncompatible) {
		_ = reopened.Close()
		t.Fatalf("expected incompatible metadata mismatch, got %v", err)
	}
	_ = reopened.Close()

	if err := Delete(context.Background(), indexRoot, "55555555-5555-5555-5555-555555555555"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	projectSidecar := filepath.Join(indexRoot, "55555555-5555-5555-5555-555555555555")
	if _, err := os.Stat(filepath.Join(projectSidecar, "CURRENT")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CURRENT should be deleted, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectSidecar, "generations")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generations should be deleted, got %v", err)
	}

	recovered, err := Open(context.Background(), OpenConfig{
		ProjectID:     "55555555-5555-5555-5555-555555555555",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open(recovered) error = %v", err)
	}
	defer recovered.Close()
	if _, err := recovered.Current(context.Background()); !errors.Is(err, ErrMissing) {
		t.Fatalf("expected missing current after delete, got %v", err)
	}
}

func TestSidecarManifestRoundTripBeforePublish(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	root := t.TempDir()
	project, err := Open(context.Background(), OpenConfig{
		ProjectID:     "88888888-8888-8888-8888-888888888888",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer project.Close()

	generation, err := project.CreateGeneration(context.Background(), 3, StateReady, nil)
	if err != nil {
		t.Fatalf("CreateGeneration() error = %v", err)
	}
	defer generation.Close()

	want := manifest.Manifest{Files: []manifest.Entry{{
		Path:                "src/main.go",
		SizeBytes:           12,
		ModTimeNs:           34,
		ContentHash:         "abc",
		Language:            "go",
		ChunkCount:          2,
		SymbolCount:         1,
		LastIndexedRevision: 3,
	}}}
	if err := generation.ReplaceManifest(want); err != nil {
		t.Fatalf("ReplaceManifest() error = %v", err)
	}
	if err := generation.Publish(context.Background()); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := generation.ReplaceManifest(manifest.Manifest{}); err == nil {
		t.Fatal("published generation must reject manifest mutation")
	}
	for _, path := range []string{
		generation.dbPath,
		filepath.Join(indexRoot, "88888888-8888-8888-8888-888888888888", "CURRENT"),
		filepath.Join(indexRoot, "88888888-8888-8888-8888-888888888888", "generations", generation.Epoch(), "state.json"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode for %q = %o, want 600", path, got)
		}
	}

	got, err := generation.Manifest()
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	if len(got.Files) != 1 || got.Files[0] != want.Files[0] {
		t.Fatalf("Manifest() = %#v, want %#v", got, want)
	}
}

func TestSidecarSearchLifecycleForStagedGeneration(t *testing.T) {
	t.Parallel()

	indexRoot := t.TempDir()
	root := t.TempDir()
	project, err := Open(context.Background(), OpenConfig{
		ProjectID:     "99999999-9999-9999-9999-999999999999",
		CanonicalRoot: root,
		IndexRoot:     indexRoot,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer project.Close()

	generation, err := project.CreateGeneration(context.Background(), 9, StateReady, nil)
	if err != nil {
		t.Fatalf("CreateGeneration() error = %v", err)
	}
	defer generation.Close()

	searchPath := generation.SearchPath()
	if searchPath == "" {
		t.Fatalf("empty search path")
	}
	contentHash := hashString("staged")
	doc := retrieval.Document{
		ID:               retrieval.DeterministicChunkID("internal/order/service.go", contentHash, 1, 1),
		Path:             "internal/order/service.go",
		Language:         "go",
		Content:          "func CreateOrder() {}",
		Snippet:          "func CreateOrder() {}",
		Identifiers:      retrieval.NormalizeAndExpandIdentifiers("CreateOrder"),
		LineStart:        1,
		LineEnd:          1,
		ContentHash:      contentHash,
		Provenance:       "fallback",
		Symbol:           "CreateOrder",
		SymbolNormalized: "createorder",
	}
	search, err := indexstore.Create(searchPath, project.projectID, generation.Epoch(), 9)
	if err != nil {
		t.Fatalf("indexstore.Create() error = %v", err)
	}
	if err := search.Apply(indexstore.ApplyRequest{
		ProjectID:     project.projectID,
		EpochID:       generation.Epoch(),
		IndexRevision: 0,
		Add:           []retrieval.Document{doc},
	}); err != nil {
		_ = search.Close()
		t.Fatalf("search.Apply() error = %v", err)
	}
	if err := search.Close(); err != nil {
		t.Fatalf("search.Close() error = %v", err)
	}

	reopened, err := indexstore.Open(searchPath, project.projectID, generation.Epoch(), 10)
	if err != nil {
		t.Fatalf("indexstore.Open(staged) error = %v", err)
	}
	results, err := reopened.Search(retrieval.SearchRequest{Query: "internal/order/service.go"})
	if err != nil {
		_ = reopened.Close()
		t.Fatalf("Search(staged) error = %v", err)
	}
	if len(results) == 0 || results[0].Path != "internal/order/service.go" {
		_ = reopened.Close()
		t.Fatalf("expected staged index hit, got %#v", results)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("reopened.Close() error = %v", err)
	}

	if _, err := project.Current(context.Background()); !errors.Is(err, ErrMissing) {
		t.Fatalf("expected unpublished generation not current, got %v", err)
	}

	if err := generation.Publish(context.Background()); err != nil {
		t.Fatalf("generation.Publish() error = %v", err)
	}

	current, err := project.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}

	published, err := indexstore.Open(current.SearchPath(), project.projectID, current.Epoch(), 10)
	if err != nil {
		_ = current.Close()
		t.Fatalf("indexstore.Open(current) error = %v", err)
	}
	if _, err := published.Search(retrieval.SearchRequest{Query: "internal/order/service.go"}); err != nil {
		_ = published.Close()
		_ = current.Close()
		t.Fatalf("Search(published) error = %v", err)
	}
	if err := published.Close(); err != nil {
		_ = current.Close()
		t.Fatalf("published.Close() error = %v", err)
	}
	if err := current.Close(); err != nil {
		t.Fatalf("current.Close() error = %v", err)
	}
}

func restoreState(t *testing.T, path string, root, epoch string, revision int64) error {
	t.Helper()

	state := State{
		ProjectID:                "55555555-5555-5555-5555-555555555555",
		ProjectRootFingerprint:   RootFingerprint(root),
		CanonicalRoot:            root,
		Epoch:                    epoch,
		Revision:                 revision,
		Status:                   StateReady,
		SidecarFormat:            SidecarFormat,
		GraphSchema:              GraphSchema,
		ParserSet:                "istok.parsers.v1",
		RankingVersion:           "istok.ranking.v1",
		CompatibilityFingerprint: defaultCompatibilityFingerprint,
		Version:                  "1",
		CreatedAt:                time.Now().UTC(),
		UpdatedAt:                time.Now().UTC(),
	}
	return writeStateAtomic(path, state)
}

func restoreStateWith(t *testing.T, path string, root, epoch string, revision int64, projectID, graphSchema string, compatibilityFingerprint string) error {
	t.Helper()

	state := State{
		ProjectID:                projectID,
		ProjectRootFingerprint:   RootFingerprint(root),
		CanonicalRoot:            root,
		Epoch:                    epoch,
		Revision:                 revision,
		Status:                   StateReady,
		SidecarFormat:            SidecarFormat,
		GraphSchema:              graphSchema,
		ParserSet:                "istok.parsers.v1",
		RankingVersion:           "istok.ranking.v1",
		CompatibilityFingerprint: compatibilityFingerprint,
		Version:                  "1",
		CreatedAt:                time.Now().UTC(),
		UpdatedAt:                time.Now().UTC(),
	}
	return writeStateAtomic(path, state)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

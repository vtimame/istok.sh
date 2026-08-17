package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"s26.dev/istok-cli/internal/codegraph"
	"s26.dev/istok-cli/internal/graphstore"
	"s26.dev/istok-cli/internal/indexing/sidecar"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/retrieval"
)

func TestEnsureFreshBuildsSemanticGraphAndSymbolChunks(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"main.go": `package main

func indexedSymbol() string { return "semantic-token" }
`,
	})

	status, err := service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() error = %v", err)
	}
	if status.State != sidecar.StateReady {
		t.Fatalf("EnsureFresh() state = %q, want %q", status.State, sidecar.StateReady)
	}

	valueManifest, _ := readCurrentManifestAndState(t, ctx, service, projectValue)
	if len(valueManifest.Files) != 1 || valueManifest.Files[0].SymbolCount == 0 {
		t.Fatalf("manifest symbol count = %#v, want positive", valueManifest.Files)
	}

	withCurrentGeneration(t, ctx, service, projectValue, func(generation *sidecar.Generation) {
		nodes, err := generation.Graph().LookupSymbols(ctx, graphstore.LookupSymbolsRequest{
			Name: "indexedSymbol",
		})
		if err != nil {
			t.Fatalf("LookupSymbols() error = %v", err)
		}
		if len(nodes) != 1 || nodes[0].Path != "main.go" {
			t.Fatalf("LookupSymbols() nodes = %#v", nodes)
		}
	})

	results, _, err := service.Search(ctx, projectValue, retrieval.SearchRequest{Query: "indexedSymbol"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || results[0].Symbol == "" {
		t.Fatalf("Search() results = %#v, want AST symbol metadata", results)
	}
}

func TestEnsureFreshParserErrorKeepsLexicalIndexAndRecoversGraph(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"broken.go": `package main

func brokenToken( {
`,
	})

	status, err := service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() parser error = %v", err)
	}
	if status.State != sidecar.StateDegraded || len(status.Diagnostics) == 0 {
		t.Fatalf("EnsureFresh() status = %#v, want degraded diagnostics", status)
	}

	results, searchStatus, err := service.Search(ctx, projectValue, retrieval.SearchRequest{Query: "brokenToken"})
	if err != nil {
		t.Fatalf("Search() degraded error = %v", err)
	}
	if searchStatus.State != sidecar.StateDegraded || len(results) == 0 {
		t.Fatalf("Search() status/results = %#v / %#v", searchStatus, results)
	}

	withCurrentGeneration(t, ctx, service, projectValue, func(generation *sidecar.Generation) {
		diagnostics, err := generation.Graph().ListDiagnostics(ctx)
		if err != nil {
			t.Fatalf("ListDiagnostics() error = %v", err)
		}
		if len(diagnostics) != 1 || diagnostics[0].Path != "broken.go" {
			t.Fatalf("ListDiagnostics() = %#v", diagnostics)
		}
	})

	writeFile(t, projectValue.Root.CanonicalPath, "broken.go", `package main

func brokenToken() string { return "recovered" }
`)
	status, err = service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() recovery error = %v", err)
	}
	if status.State != sidecar.StateReady || len(status.Diagnostics) != 0 {
		t.Fatalf("EnsureFresh() recovered status = %#v", status)
	}

	withCurrentGeneration(t, ctx, service, projectValue, func(generation *sidecar.Generation) {
		diagnostics, err := generation.Graph().ListDiagnostics(ctx)
		if err != nil {
			t.Fatalf("ListDiagnostics() recovered error = %v", err)
		}
		if len(diagnostics) != 0 {
			t.Fatalf("ListDiagnostics() recovered = %#v, want empty", diagnostics)
		}
	})
}

func TestEnsureFreshDeletesAndRenamesGraphFacts(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"old.go": `package main

func renamedSymbol() {}
`,
		"deleted.go": `package main

func deletedSymbol() {}
`,
	})

	if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
		t.Fatalf("EnsureFresh() initial error = %v", err)
	}
	if err := os.Rename(
		filepath.Join(projectValue.Root.CanonicalPath, "old.go"),
		filepath.Join(projectValue.Root.CanonicalPath, "new.go"),
	); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := os.Remove(filepath.Join(projectValue.Root.CanonicalPath, "deleted.go")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
		t.Fatalf("EnsureFresh() refresh error = %v", err)
	}

	withCurrentGeneration(t, ctx, service, projectValue, func(generation *sidecar.Generation) {
		deleted, err := generation.Graph().LookupSymbols(ctx, graphstore.LookupSymbolsRequest{Name: "deletedSymbol"})
		if err != nil {
			t.Fatalf("LookupSymbols(deleted) error = %v", err)
		}
		if len(deleted) != 0 {
			t.Fatalf("LookupSymbols(deleted) = %#v", deleted)
		}

		renamed, err := generation.Graph().LookupSymbols(ctx, graphstore.LookupSymbolsRequest{Name: "renamedSymbol"})
		if err != nil {
			t.Fatalf("LookupSymbols(renamed) error = %v", err)
		}
		if len(renamed) != 1 || renamed[0].Path != "new.go" {
			t.Fatalf("LookupSymbols(renamed) = %#v", renamed)
		}
	})
}

func TestEnsureFreshResolvesUnambiguousCrossFileCall(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"caller.go": `package service

func caller() { target() }
`,
		"target.go": `package service

func target() {}
`,
	})

	if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
		t.Fatalf("EnsureFresh() error = %v", err)
	}

	withCurrentGeneration(t, ctx, service, projectValue, func(generation *sidecar.Generation) {
		callers, err := generation.Graph().LookupSymbols(ctx, graphstore.LookupSymbolsRequest{Name: "caller"})
		if err != nil {
			t.Fatalf("LookupSymbols(caller) error = %v", err)
		}
		if len(callers) != 1 {
			t.Fatalf("LookupSymbols(caller) = %#v", callers)
		}

		neighbors, err := generation.Graph().Neighbors(ctx, graphstore.NeighborsRequest{
			SourceID: callers[0].ID,
			Kinds:    []codegraph.EdgeKind{codegraph.EdgeCalls},
		})
		if err != nil {
			t.Fatalf("Neighbors(calls) error = %v", err)
		}
		if len(neighbors) != 1 || neighbors[0].Name != "target" || neighbors[0].Path != "target.go" {
			t.Fatalf("Neighbors(calls) = %#v", neighbors)
		}
	})
}

func withCurrentGeneration(
	t *testing.T,
	ctx context.Context,
	service *Service,
	value project.Project,
	check func(*sidecar.Generation),
) {
	t.Helper()

	handle, err := sidecar.Open(ctx, sidecar.OpenConfig{
		ProjectID:     value.ID,
		CanonicalRoot: value.Root.CanonicalPath,
		IndexRoot:     service.config.IndexRoot,
	})
	if err != nil {
		t.Fatalf("sidecar.Open() error = %v", err)
	}
	defer handle.Close()

	generation, err := handle.Current(ctx)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	defer generation.Close()

	check(generation)
}

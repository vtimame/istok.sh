package indexing

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/vtimame/istok.sh/internal/codegraph"
	"github.com/vtimame/istok.sh/internal/indexing/discovery"
	"github.com/vtimame/istok.sh/internal/indexing/sidecar"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

func TestRebuildAlwaysPublishesNewEpochAndKeepsCurrentOnFailure(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"main.go": "package main\n\nfunc stable() {}\n",
	})

	initial, err := service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() error = %v", err)
	}

	rebuilt, err := service.Rebuild(ctx, projectValue)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if rebuilt.State != sidecar.StateReady || rebuilt.EpochID == initial.EpochID {
		t.Fatalf("Rebuild() status = %#v, initial = %#v", rebuilt, initial)
	}

	previousEpoch := rebuilt.EpochID
	service.discover = func(context.Context, string, discovery.Previous) (discovery.Result, error) {
		return discovery.Result{}, errors.New("forced rebuild failure")
	}
	failed, err := service.Rebuild(ctx, projectValue)
	if err == nil || !IsError(err) {
		t.Fatalf("Rebuild() error = %v, want indexing.Error", err)
	}
	if failed.State != sidecar.StateReady || failed.EpochID != previousEpoch {
		t.Fatalf("failed Rebuild() status = %#v, want readable epoch %q", failed, previousEpoch)
	}

	service.discover = discovery.DiscoverWithPrevious
	current, err := service.Status(ctx, projectValue)
	if err != nil {
		t.Fatalf("Status() after failed rebuild error = %v", err)
	}
	if current.State != sidecar.StateReady || current.EpochID != previousEpoch {
		t.Fatalf("current status = %#v, want readable epoch %q", current, previousEpoch)
	}
}

func TestSymbolsRefreshesAndPrioritizesQualifiedName(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"one.go": "package one\n\nfunc shared() {}\n",
		"two.go": "package two\n\nfunc shared() {}\n",
	})

	if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
		t.Fatalf("EnsureFresh() error = %v", err)
	}

	short, _, err := service.Symbols(ctx, projectValue, SymbolRequest{Name: "shared"})
	if err != nil || len(short) != 2 {
		t.Fatalf("Symbols(short) = %#v, %v", short, err)
	}
	qualified := short[1].QualifiedName
	if qualified == "" {
		t.Fatalf("expected qualified symbol, got %#v", short)
	}

	matched, status, err := service.Symbols(ctx, projectValue, SymbolRequest{Name: qualified})
	if err != nil {
		t.Fatalf("Symbols(qualified) error = %v", err)
	}
	if status.State != sidecar.StateReady || len(matched) == 0 || matched[0].QualifiedName != qualified {
		t.Fatalf("Symbols(qualified) status/nodes = %#v / %#v", status, matched)
	}

	writeFile(t, projectValue.Root.CanonicalPath, "three.go", "package three\n\nfunc freshSymbol() {}\n")
	fresh, _, err := service.Symbols(ctx, projectValue, SymbolRequest{Name: "freshSymbol"})
	if err != nil || len(fresh) != 1 || fresh[0].Path != "three.go" {
		t.Fatalf("Symbols() did not refresh: %#v, %v", fresh, err)
	}
}

func TestNeighborsAndPathsPreserveGraphMetadataAndBounds(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"main.go": "package main\n\nfunc caller() { target() }\nfunc target() {}\n",
	})

	neighbors, status, err := service.Neighbors(ctx, projectValue, NeighborsRequest{
		Name:  "caller",
		Kinds: []codegraph.EdgeKind{codegraph.EdgeCalls},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("Neighbors() error = %v", err)
	}
	if status.State != sidecar.StateReady || len(neighbors) != 1 {
		t.Fatalf("Neighbors() status/neighbors = %#v / %#v", status, neighbors)
	}
	neighbor := neighbors[0]
	if neighbor.Source.Name != "caller" || neighbor.Target.Name != "target" || neighbor.Kind != codegraph.EdgeCalls || neighbor.Provenance == "" || neighbor.EvidencePath != "main.go" || neighbor.EvidenceLine == 0 {
		t.Fatalf("neighbor metadata = %#v", neighbor)
	}

	paths, _, err := service.Paths(ctx, projectValue, PathRequest{From: "caller", To: "target", MaxDepth: 1, Limit: 1})
	if err != nil {
		t.Fatalf("Paths() error = %v", err)
	}
	if len(paths) != 1 || len(paths[0].Nodes) != 2 || paths[0].Nodes[0].Name != "caller" || paths[0].Nodes[1].Name != "target" {
		t.Fatalf("Paths() = %#v", paths)
	}
}

func TestGraphQueriesPropagateDegradedStatus(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"valid.go":  "package main\n\nfunc available() {}\n",
		"broken.go": "package main\n\nfunc broken( {\n",
	})

	nodes, status, err := service.Symbols(ctx, projectValue, SymbolRequest{Name: "available"})
	if err != nil {
		t.Fatalf("Symbols() error = %v", err)
	}
	if status.State != sidecar.StateDegraded || len(status.Diagnostics) == 0 || len(nodes) != 1 {
		t.Fatalf("Symbols() status/nodes = %#v / %#v", status, nodes)
	}
}

func TestGraphRequestsValidateBeforeSidecarWork(t *testing.T) {
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, nil)

	_, _, err := service.Neighbors(context.Background(), projectValue, NeighborsRequest{
		Name:  "source",
		Kinds: []codegraph.EdgeKind{"invalid"},
	})
	if err == nil || !IsError(err) {
		t.Fatalf("Neighbors() error = %v, want indexing.Error", err)
	}
	entries, readErr := os.ReadDir(service.config.IndexRoot)
	if readErr != nil {
		t.Fatalf("ReadDir() error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid request created sidecar entries: %#v", entries)
	}

	_, _, err = service.Paths(context.Background(), projectValue, PathRequest{From: "a", To: "b", MaxDepth: -1})
	if err == nil || !IsError(err) {
		t.Fatalf("Paths() error = %v, want indexing.Error", err)
	}

	_, _, err = service.Search(context.Background(), projectValue, retrieval.SearchRequest{
		Query: "source",
		Limit: maxSearchLimit + 1,
	})
	if err == nil || !IsError(err) {
		t.Fatalf("Search() error = %v, want indexing.Error", err)
	}

	entries, readErr = os.ReadDir(service.config.IndexRoot)
	if readErr != nil {
		t.Fatalf("ReadDir() after invalid requests error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid requests created sidecar entries: %#v", entries)
	}
}

package indexing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"s26.dev/istok-cli/internal/codegraph"
	"s26.dev/istok-cli/internal/indexing/chunker"
	"s26.dev/istok-cli/internal/indexing/discovery"
	"s26.dev/istok-cli/internal/indexing/manifest"
	"s26.dev/istok-cli/internal/indexing/sidecar"
	"s26.dev/istok-cli/internal/indexstore"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/retrieval"
)

const maxUpdatePasses = 2

var errSourceChanged = errors.New("source changed while indexing")

type activeIndex struct {
	generation *sidecar.Generation
	store      *indexstore.LexicalStore
	state      sidecar.State
	manifest   manifest.Manifest
}

type buildArtifacts struct {
	documents    []retrieval.Document
	chunkCounts  map[string]int64
	symbolCounts map[string]int64
	graphs       []codegraph.FileGraph
	diagnostics  []codegraph.Diagnostic
}

func (a *activeIndex) close() error {
	var result error
	if a.store != nil {
		result = errors.Join(result, a.store.Close())
	}
	if a.generation != nil {
		result = errors.Join(result, a.generation.Close())
	}

	return result
}

func (s *Service) ensureFresh(ctx context.Context, value project.Project) (Status, error) {
	indexedProject, err := s.openProject(ctx, value)
	if err != nil {
		status := failedStatus(err)
		return status, wrapError(status, err)
	}
	defer indexedProject.Close()

	active, err := s.openActive(ctx, indexedProject, value)
	passes := 0
	confirmed := false
	if err != nil {
		active, err = s.rebuild(ctx, indexedProject, value)
		if err != nil {
			status := failedStatus(err)
			failedState, stateErr := s.recordFailure(ctx, indexedProject, err)
			if stateErr == nil {
				status = statusFromState(failedState)
			} else {
				err = errors.Join(err, fmt.Errorf("record failed index state: %w", stateErr))
				status.Diagnostics = []string{err.Error()}
			}

			return status, wrapError(status, err)
		}

		passes = 1
		confirmed = true
	}
	defer active.close()
	if err := indexedProject.CleanupGenerations(active.generation.Epoch()); err != nil {
		status := statusFromState(active.state)
		return status, wrapError(status, err)
	}

	unstableReads := 0
	for {
		next, diff, files, err := s.scan(ctx, value.Root.CanonicalPath, active.manifest, active.state.Revision+1)
		if err != nil {
			status := statusFromState(active.state)
			if markErr := active.generation.MarkStatus(sidecar.StateStale, []string{err.Error()}); markErr != nil {
				err = errors.Join(err, fmt.Errorf("mark index stale: %w", markErr))
				status.State = sidecar.StateFailed
			} else {
				status.State = sidecar.StateStale
				status.Diagnostics = []string{err.Error()}
			}

			return status, wrapError(status, err)
		}

		if !manifestChanged(active.manifest, next) {
			if confirmed {
				return statusFromState(active.state), nil
			}

			confirmed = true
			continue
		}

		confirmed = true
		if passes >= maxUpdatePasses {
			err := errors.New("files kept changing after two index update passes")
			status := statusFromState(active.state)
			if markErr := active.generation.MarkStatus(sidecar.StateStale, []string{err.Error()}); markErr != nil {
				err = errors.Join(err, fmt.Errorf("mark index stale: %w", markErr))
				status.State = sidecar.StateFailed
			} else {
				status.State = sidecar.StateStale
				status.Diagnostics = []string{err.Error()}
			}

			return status, wrapError(status, err)
		}

		if err := s.apply(ctx, value, active, next, diff, files); err != nil {
			if errors.Is(err, errSourceChanged) {
				unstableReads++
				if unstableReads < maxUpdatePasses {
					continue
				}

				status := statusFromState(active.state)
				if markErr := active.generation.MarkStatus(sidecar.StateStale, []string{err.Error()}); markErr != nil {
					err = errors.Join(err, fmt.Errorf("mark index stale: %w", markErr))
					status.State = sidecar.StateFailed
				} else {
					status.State = sidecar.StateStale
					status.Diagnostics = []string{err.Error()}
				}

				return status, wrapError(status, err)
			}

			status := statusFromState(active.state)
			status.State = sidecar.StateUpdating
			status.Diagnostics = []string{err.Error()}
			return status, wrapError(status, err)
		}

		passes++
		unstableReads = 0
	}
}

func (s *Service) recordFailure(ctx context.Context, indexedProject *sidecar.Project, failure error) (sidecar.State, error) {
	generation, err := indexedProject.CreateGeneration(ctx, 0, sidecar.StateFailed, []string{failure.Error()})
	if err != nil {
		return sidecar.State{}, err
	}
	defer generation.Close()

	if err := generation.Publish(ctx); err != nil {
		return sidecar.State{}, err
	}

	return generation.State()
}

func (s *Service) rebuild(ctx context.Context, indexedProject *sidecar.Project, value project.Project) (*activeIndex, error) {
	var lastErr error
	for attempt := 0; attempt < maxUpdatePasses; attempt++ {
		active, err := s.fullBuild(ctx, indexedProject, value)
		if err == nil {
			return active, nil
		}
		lastErr = err
		if !errors.Is(err, errSourceChanged) {
			break
		}
	}

	return nil, lastErr
}

func (s *Service) openActive(ctx context.Context, indexedProject *sidecar.Project, value project.Project) (*activeIndex, error) {
	generation, err := indexedProject.CurrentForUpdate(ctx)
	if err != nil {
		return nil, err
	}

	state, err := generation.State()
	if err != nil {
		generation.Close()
		return nil, err
	}
	if state.Status == sidecar.StateUpdating || state.Status == sidecar.StateFailed {
		generation.Close()
		return nil, fmt.Errorf("current generation requires rebuild in state %q", state.Status)
	}

	valueManifest, err := generation.Manifest()
	if err != nil {
		generation.Close()
		return nil, err
	}

	store, err := indexstore.Open(generation.SearchPath(), value.ID, generation.Epoch(), state.Revision)
	if err != nil {
		generation.Close()
		return nil, err
	}

	return &activeIndex{generation: generation, store: store, state: state, manifest: valueManifest}, nil
}

func (s *Service) fullBuild(ctx context.Context, indexedProject *sidecar.Project, value project.Project) (_ *activeIndex, resultErr error) {
	filesResult, err := s.discover(ctx, value.Root.CanonicalPath, nil)
	if err != nil {
		return nil, fmt.Errorf("discover initial index: %w", err)
	}
	if err := incompleteDiscovery(filesResult.Diagnostics); err != nil {
		return nil, err
	}

	valueManifest, _, err := manifest.Build(manifest.Manifest{}, filesResult.Files, 1)
	if err != nil {
		return nil, err
	}
	artifacts, err := s.buildArtifacts(filesResult.Files, allPaths(valueManifest))
	if err != nil {
		return nil, err
	}
	applyIndexCounts(&valueManifest, artifacts.chunkCounts, artifacts.symbolCounts, 1)
	finalStatus, diagnostics := graphStatus(artifacts.diagnostics)

	generation, err := indexedProject.CreateGeneration(ctx, 1, finalStatus, diagnostics)
	if err != nil {
		return nil, err
	}
	published := false
	defer func() {
		if resultErr != nil || !published {
			resultErr = errors.Join(resultErr, generation.Close())
		}
	}()

	if err := generation.ReplaceManifestAndGraph(
		ctx,
		valueManifest,
		sortedPathSet(allPaths(valueManifest)),
		artifacts.graphs,
	); err != nil {
		return nil, err
	}

	store, err := indexstore.Create(generation.SearchPath(), value.ID, generation.Epoch(), 0)
	if err != nil {
		return nil, err
	}
	storeOpen := true
	defer func() {
		if resultErr != nil && storeOpen {
			resultErr = errors.Join(resultErr, store.Close())
		}
	}()

	if err := store.Apply(retrieval.ApplyRequest{
		ProjectID:     value.ID,
		EpochID:       generation.Epoch(),
		IndexRevision: 1,
		Add:           artifacts.documents,
	}); err != nil {
		return nil, err
	}
	if err := store.Close(); err != nil {
		return nil, err
	}
	storeOpen = false

	store, err = indexstore.Open(generation.SearchPath(), value.ID, generation.Epoch(), 1)
	if err != nil {
		return nil, fmt.Errorf("verify lexical index: %w", err)
	}
	storeOpen = true
	if err := generation.Publish(ctx); err != nil {
		return nil, err
	}
	published = true

	state, err := generation.State()
	if err != nil {
		return nil, err
	}

	storeOpen = false
	return &activeIndex{generation: generation, store: store, state: state, manifest: valueManifest}, nil
}

func (s *Service) scan(ctx context.Context, root string, previous manifest.Manifest, revision int64) (manifest.Manifest, manifest.Diff, []discovery.File, error) {
	filesResult, err := s.discover(ctx, root, previousForDiscovery(previous))
	if err != nil {
		return manifest.Manifest{}, manifest.Diff{}, nil, fmt.Errorf("discover index refresh: %w", err)
	}
	if err := incompleteDiscovery(filesResult.Diagnostics); err != nil {
		return manifest.Manifest{}, manifest.Diff{}, nil, err
	}

	next, diff, err := manifest.Build(previous, filesResult.Files, revision)
	if err != nil {
		return manifest.Manifest{}, manifest.Diff{}, nil, err
	}

	return next, diff, filesResult.Files, nil
}

func (s *Service) apply(ctx context.Context, value project.Project, active *activeIndex, next manifest.Manifest, diff manifest.Diff, files []discovery.File) error {
	targetRevision := active.state.Revision + 1
	changed := changedPaths(diff)
	artifacts, err := s.buildArtifacts(files, changed)
	if err != nil {
		return err
	}
	applyIndexCounts(&next, artifacts.chunkCounts, artifacts.symbolCounts, targetRevision)

	replacePaths := append(sortedPathSet(changed), deletedPaths(diff)...)
	sort.Strings(replacePaths)
	diagnostics, err := mergedGraphDiagnostics(ctx, active.generation, replacePaths, artifacts.diagnostics)
	if err != nil {
		return err
	}
	finalStatus, diagnosticMessages := graphStatus(diagnostics)

	if err := active.generation.BeginUpdate(targetRevision); err != nil {
		return err
	}
	if err := active.generation.CommitUpdateWithGraph(
		ctx,
		next,
		targetRevision,
		finalStatus,
		replacePaths,
		artifacts.graphs,
	); err != nil {
		return err
	}
	if err := active.store.Apply(retrieval.ApplyRequest{
		ProjectID:     value.ID,
		EpochID:       active.generation.Epoch(),
		IndexRevision: targetRevision,
		Add:           artifacts.documents,
		DeletePaths:   deletedPaths(diff),
	}); err != nil {
		return err
	}
	if err := active.generation.CompleteUpdate(targetRevision, finalStatus, diagnosticMessages); err != nil {
		return err
	}

	state, err := active.generation.State()
	if err != nil {
		return err
	}
	active.state = state
	active.manifest = next

	return nil
}

func (s *Service) buildArtifacts(files []discovery.File, paths map[string]struct{}) (buildArtifacts, error) {
	byPath := make(map[string]discovery.File, len(files))
	for _, file := range files {
		byPath[file.Path] = file
	}

	sortedPaths := make([]string, 0, len(paths))
	for path := range paths {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)

	result := buildArtifacts{
		documents:    make([]retrieval.Document, 0),
		chunkCounts:  make(map[string]int64, len(sortedPaths)),
		symbolCounts: make(map[string]int64, len(sortedPaths)),
		graphs:       make([]codegraph.FileGraph, 0, len(sortedPaths)),
	}
	analyzer := codegraph.NewAnalyzer()

	for _, path := range sortedPaths {
		file, exists := byPath[path]
		if !exists {
			return buildArtifacts{}, fmt.Errorf("%w: discovered file %q disappeared", errSourceChanged, path)
		}

		content, err := s.readFile(file.AbsPath)
		if err != nil {
			return buildArtifacts{}, fmt.Errorf("%w: read %q: %v", errSourceChanged, path, err)
		}
		hash := sha256.Sum256(content)
		if hex.EncodeToString(hash[:]) != file.ContentHash {
			return buildArtifacts{}, fmt.Errorf("%w: content hash changed for %q", errSourceChanged, path)
		}

		var symbols []chunker.SymbolChunk
		if analyzer.Supports(file.Language) {
			graph, err := analyzer.Analyze(path, file.Language, content, file.ContentHash)
			if err != nil {
				return buildArtifacts{}, fmt.Errorf("analyze %q: %w", path, err)
			}

			result.graphs = append(result.graphs, graph)
			result.diagnostics = append(result.diagnostics, graph.Diagnostics...)
			symbols = graphChunkSymbols(graph)
			result.symbolCounts[path] = int64(len(symbols))
		}

		chunks, err := chunker.Chunks(path, file.Language, content, file.ContentHash, symbols)
		if err != nil {
			return buildArtifacts{}, fmt.Errorf("chunk %q: %w", path, err)
		}
		result.documents = append(result.documents, chunks...)
		result.chunkCounts[path] = int64(len(chunks))
	}

	return result, nil
}

func graphChunkSymbols(graph codegraph.FileGraph) []chunker.SymbolChunk {
	if len(graph.Diagnostics) > 0 {
		return nil
	}

	result := make([]chunker.SymbolChunk, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		switch node.Kind {
		case codegraph.NodeType, codegraph.NodeInterface, codegraph.NodeFunction, codegraph.NodeMethod:
		default:
			continue
		}

		symbol := strings.TrimSpace(node.QualifiedName)
		if symbol == "" {
			symbol = strings.TrimSpace(node.Name)
		}
		if symbol == "" || node.LineStart <= 0 || node.LineEnd < node.LineStart {
			continue
		}

		result = append(result, chunker.SymbolChunk{
			Symbol:    symbol,
			LineStart: node.LineStart,
			LineEnd:   node.LineEnd,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].LineStart != result[j].LineStart {
			return result[i].LineStart < result[j].LineStart
		}
		if result[i].LineEnd != result[j].LineEnd {
			return result[i].LineEnd < result[j].LineEnd
		}
		return result[i].Symbol < result[j].Symbol
	})

	return result
}

func previousForDiscovery(value manifest.Manifest) discovery.Previous {
	result := make(discovery.Previous, len(value.Files))
	for _, file := range value.Files {
		result[file.Path] = discovery.PreviousFile{
			SizeBytes:   file.SizeBytes,
			ModTimeNs:   file.ModTimeNs,
			ContentHash: file.ContentHash,
		}
	}

	return result
}

func incompleteDiscovery(diagnostics []discovery.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		if diagnostic.Reason == discovery.SkipUnlisted {
			return fmt.Errorf("discovery incomplete at %q: %s", diagnostic.Path, diagnostic.Detail)
		}
	}

	return nil
}

func manifestChanged(previous, next manifest.Manifest) bool {
	if len(previous.Files) != len(next.Files) {
		return true
	}
	for i := range previous.Files {
		if previous.Files[i] != next.Files[i] {
			return true
		}
	}

	return false
}

func changedPaths(diff manifest.Diff) map[string]struct{} {
	result := make(map[string]struct{}, len(diff.Added)+len(diff.Modified)+len(diff.Renamed))
	for _, path := range diff.Added {
		result[path] = struct{}{}
	}
	for _, path := range diff.Modified {
		result[path] = struct{}{}
	}
	for _, rename := range diff.Renamed {
		result[rename.To] = struct{}{}
	}

	return result
}

func deletedPaths(diff manifest.Diff) []string {
	result := append([]string(nil), diff.Deleted...)
	for _, rename := range diff.Renamed {
		result = append(result, rename.From)
	}
	sort.Strings(result)

	return result
}

func allPaths(value manifest.Manifest) map[string]struct{} {
	result := make(map[string]struct{}, len(value.Files))
	for _, file := range value.Files {
		result[file.Path] = struct{}{}
	}

	return result
}

func applyIndexCounts(value *manifest.Manifest, chunkCounts, symbolCounts map[string]int64, revision int64) {
	for index := range value.Files {
		chunkCount, exists := chunkCounts[value.Files[index].Path]
		if !exists {
			continue
		}

		value.Files[index].ChunkCount = chunkCount
		value.Files[index].SymbolCount = symbolCounts[value.Files[index].Path]
		value.Files[index].LastIndexedRevision = revision
	}
}

func sortedPathSet(paths map[string]struct{}) []string {
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)

	return result
}

func mergedGraphDiagnostics(
	ctx context.Context,
	generation *sidecar.Generation,
	replacePaths []string,
	fresh []codegraph.Diagnostic,
) ([]codegraph.Diagnostic, error) {
	existing, err := generation.Graph().ListDiagnostics(ctx)
	if err != nil {
		return nil, fmt.Errorf("read graph diagnostics: %w", err)
	}

	replaced := make(map[string]struct{}, len(replacePaths))
	for _, path := range replacePaths {
		replaced[path] = struct{}{}
	}

	result := make([]codegraph.Diagnostic, 0, len(existing)+len(fresh))
	for _, diagnostic := range existing {
		if _, exists := replaced[diagnostic.Path]; exists {
			continue
		}
		result = append(result, diagnostic)
	}
	result = append(result, fresh...)

	return result, nil
}

func graphStatus(diagnostics []codegraph.Diagnostic) (sidecar.StateStatus, []string) {
	if len(diagnostics) == 0 {
		return sidecar.StateReady, nil
	}

	values := append([]codegraph.Diagnostic(nil), diagnostics...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].Path != values[j].Path {
			return values[i].Path < values[j].Path
		}
		if values[i].Line != values[j].Line {
			return values[i].Line < values[j].Line
		}
		return values[i].Message < values[j].Message
	})

	result := make([]string, 0, len(values))
	for _, diagnostic := range values {
		location := diagnostic.Path
		if diagnostic.Line > 0 {
			location = fmt.Sprintf("%s:%d", location, diagnostic.Line)
		}
		result = append(result, fmt.Sprintf("%s: %s", location, diagnostic.Message))
	}

	return sidecar.StateDegraded, result
}

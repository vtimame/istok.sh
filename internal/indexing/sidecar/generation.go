package sidecar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"s26.dev/istok-cli/internal/codegraph"
	"s26.dev/istok-cli/internal/graphstore"
	"s26.dev/istok-cli/internal/indexing/manifest"
)

// CreateGeneration prepares an unpublished generation. Call Publish only after
// all derived stores for the generation have been populated and verified.
func (p *Project) CreateGeneration(ctx context.Context, revision int64, status StateStatus, diagnostics []string) (*Generation, error) {
	if p == nil {
		return nil, errors.New("nil sidecar project")
	}
	if status == "" {
		status = StateReady
	}
	if revision < 0 {
		return nil, errors.New("revision must not be negative")
	}
	if !validStateStatus(status) {
		return nil, fmt.Errorf("invalid state status %q", status)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	epoch := uuid.NewString()
	generationDir := filepath.Join(p.projectDir(), "generations", epoch)
	if err := os.MkdirAll(generationDir, directoryMode); err != nil {
		return nil, fmt.Errorf("create generation directory: %w", err)
	}
	if err := os.Chmod(generationDir, directoryMode); err != nil {
		return nil, fmt.Errorf("set generation directory permissions: %w", err)
	}

	now := p.now().UTC()
	state := State{
		ProjectID:                p.projectID,
		ProjectRootFingerprint:   p.rootFingerprint,
		CanonicalRoot:            p.canonicalRoot,
		Epoch:                    epoch,
		Revision:                 revision,
		Status:                   status,
		SidecarFormat:            SidecarFormat,
		GraphSchema:              GraphSchema,
		ParserSet:                defaultParserSet,
		RankingVersion:           defaultRanking,
		CompatibilityFingerprint: defaultCompatibilityFingerprint,
		Version:                  defaultStateVersion,
		CreatedAt:                now,
		UpdatedAt:                now,
		Diagnostics:              limitDiagnostics(diagnostics),
	}

	graphPath := filepath.Join(generationDir, "graph.db")
	db, err := openGraphDB(graphPath)
	if err != nil {
		return nil, err
	}
	if err := initializeGraph(db, state); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := writeStateAtomic(filepath.Join(generationDir, "state.json"), state); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Generation{project: p, epoch: epoch, db: db, dbPath: graphPath}, nil
}

// Current opens active generation through CURRENT and validates metadata markers.
func (p *Project) Current(ctx context.Context) (*Generation, error) {
	return p.current(ctx, openGraphDBRead)
}

// CurrentForUpdate opens the published generation and returns a writable sqlite handle.
func (p *Project) CurrentForUpdate(ctx context.Context) (*Generation, error) {
	return p.current(ctx, openGraphDB)
}

// InspectState reads the published state marker without requiring graph or
// lexical markers to match. It is intended for status reporting and recovery
// diagnostics; callers must not use it to authorize reads.
func (p *Project) InspectState(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	epoch, err := readCurrent(filepath.Join(p.projectDir(), "CURRENT"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, classifyError(ProblemMissing, "missing CURRENT", err)
		}

		return State{}, classifyError(ProblemCorrupt, "read CURRENT", err)
	}
	epoch = strings.TrimSpace(epoch)
	if _, err := uuid.Parse(epoch); err != nil {
		return State{}, classifyError(ProblemCorrupt, "CURRENT epoch is not UUID", err)
	}

	statePath := filepath.Join(p.projectDir(), "generations", epoch, "state.json")
	state, err := readState(statePath)
	if err != nil {
		return State{}, classifyError(ProblemCorrupt, "read state.json", err)
	}
	if err := validateStateConstants(state); err != nil {
		return State{}, err
	}
	if state.ProjectID != p.projectID || state.Epoch != epoch {
		return State{}, classifyError(ProblemIncompatible, "generation identity mismatch", nil)
	}
	if state.ProjectRootFingerprint != p.rootFingerprint || state.CanonicalRoot != p.canonicalRoot {
		return State{}, classifyError(ProblemRebind, "project root changed", nil)
	}

	return state, nil
}

// CleanupGenerations removes obsolete and interrupted generations while the
// project lock is held, preserving the supplied current epoch.
func (p *Project) CleanupGenerations(keepEpoch string) error {
	if p == nil {
		return errors.New("nil sidecar project")
	}
	if _, err := uuid.Parse(keepEpoch); err != nil {
		return fmt.Errorf("invalid generation to keep: %w", err)
	}

	root := filepath.Join(p.projectDir(), "generations")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("read generations: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == keepEpoch {
			continue
		}

		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return fmt.Errorf("remove generation %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func (p *Project) current(ctx context.Context, openGraph func(string) (*sql.DB, error)) (*Generation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	epoch, err := readCurrent(filepath.Join(p.projectDir(), "CURRENT"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, classifyError(ProblemMissing, "missing CURRENT", err)
		}

		return nil, classifyError(ProblemCorrupt, "read CURRENT", err)
	}
	epoch = strings.TrimSpace(epoch)
	if epoch == "" {
		return nil, classifyError(ProblemCorrupt, "empty CURRENT", nil)
	}
	if _, err := uuid.Parse(epoch); err != nil {
		return nil, classifyError(ProblemCorrupt, "CURRENT epoch is not UUID", err)
	}

	generationDir := filepath.Join(p.projectDir(), "generations", epoch)
	statePath := filepath.Join(generationDir, "state.json")
	graphPath := filepath.Join(generationDir, "graph.db")

	if err := ensureExists(statePath); err != nil {
		return nil, classifyError(ProblemCorrupt, "state.json missing", err)
	}
	if err := ensureExists(graphPath); err != nil {
		return nil, classifyError(ProblemCorrupt, "graph.db missing", err)
	}
	db, err := openGraph(graphPath)
	if err != nil {
		return nil, classifyError(ProblemCorrupt, "open graph db", err)
	}

	state, err := readState(statePath)
	if err != nil {
		_ = db.Close()
		return nil, classifyError(ProblemCorrupt, "malformed state.json", err)
	}
	if err := validateStateConstants(state); err != nil {
		_ = db.Close()
		return nil, err
	}

	if state.ProjectID != p.projectID {
		_ = db.Close()
		return nil, classifyError(ProblemIncompatible, "project id mismatch", nil)
	}
	if state.ProjectRootFingerprint != p.rootFingerprint || state.CanonicalRoot != p.canonicalRoot {
		_ = db.Close()
		return nil, classifyError(ProblemRebind, "project root changed", nil)
	}
	if state.Epoch != epoch {
		_ = db.Close()
		return nil, classifyError(ProblemIncompatible, "CURRENT epoch mismatch", nil)
	}

	metadata, err := readGraphMetadata(db)
	if err != nil {
		_ = db.Close()
		return nil, classifyError(ProblemCorrupt, "read graph metadata", err)
	}
	if !graphMetadataMatches(state, metadata) {
		_ = db.Close()
		return nil, classifyError(ProblemIncompatible, "state and graph metadata mismatch", nil)
	}

	return &Generation{project: p, epoch: epoch, db: db, dbPath: graphPath, published: true}, nil
}

// Generation represents one sidecar generation.
type Generation struct {
	project   *Project
	epoch     string
	dbPath    string
	db        *sql.DB
	published bool
}

func (g *Generation) Close() error {
	if g == nil || g.db == nil {
		return nil
	}
	return g.db.Close()
}

func (g *Generation) Epoch() string { return g.epoch }

func (g *Generation) SearchPath() string {
	if g == nil || g.project == nil {
		return ""
	}
	return filepath.Join(g.project.projectDir(), "generations", g.epoch, "search.bleve")
}

// Graph provides read access to semantic graph facts in this generation.
func (g *Generation) Graph() *graphstore.Repository {
	if g == nil || g.db == nil {
		return nil
	}

	return graphstore.New(g.db)
}

func (g *Generation) State() (State, error) {
	if g == nil || g.project == nil {
		return State{}, errors.New("generation has no project")
	}
	statePath := filepath.Join(g.project.projectDir(), "generations", g.epoch, "state.json")
	return readState(statePath)
}

// BeginUpdate records an interrupted-update marker before either derived store
// advances. The committed revision remains unchanged until CompleteUpdate.
func (g *Generation) BeginUpdate(targetRevision int64) error {
	if err := g.requirePublished(); err != nil {
		return err
	}

	state, err := g.State()
	if err != nil {
		return fmt.Errorf("read generation state: %w", err)
	}
	if state.Status == StateUpdating {
		return errors.New("generation update is already in progress")
	}
	if targetRevision <= state.Revision {
		return fmt.Errorf("target revision %d must be greater than committed revision %d", targetRevision, state.Revision)
	}

	state.Status = StateUpdating
	state.TargetRevision = &targetRevision
	state.UpdatedAt = g.project.now().UTC()

	return g.writeState(state)
}

// CommitUpdate atomically replaces the manifest and advances graph metadata to
// the target revision. state.json intentionally remains updating until Bleve is
// committed and CompleteUpdate is called.
func (g *Generation) CommitUpdate(value manifest.Manifest, targetRevision int64, finalStatus StateStatus) error {
	return g.commitUpdate(context.Background(), value, targetRevision, finalStatus, nil, nil)
}

// CommitUpdateWithGraph atomically advances the manifest, graph and graph markers.
func (g *Generation) CommitUpdateWithGraph(
	ctx context.Context,
	value manifest.Manifest,
	targetRevision int64,
	finalStatus StateStatus,
	replacePaths []string,
	files []codegraph.FileGraph,
) error {
	return g.commitUpdate(ctx, value, targetRevision, finalStatus, replacePaths, files)
}

func (g *Generation) commitUpdate(
	ctx context.Context,
	value manifest.Manifest,
	targetRevision int64,
	finalStatus StateStatus,
	replacePaths []string,
	files []codegraph.FileGraph,
) error {
	if err := g.requirePublished(); err != nil {
		return err
	}
	if finalStatus != StateReady && finalStatus != StateDegraded {
		return fmt.Errorf("invalid committed status %q", finalStatus)
	}

	state, err := g.State()
	if err != nil {
		return fmt.Errorf("read generation state: %w", err)
	}
	if state.Status != StateUpdating || state.TargetRevision == nil || *state.TargetRevision != targetRevision {
		return errors.New("generation update target does not match state")
	}

	return replaceManifestGraphAndMarkers(ctx, g.db, value, targetRevision, finalStatus, replacePaths, files)
}

// CompleteUpdate publishes the committed revision in state.json after both
// graph.db and Bleve have advanced to the same target revision.
func (g *Generation) CompleteUpdate(targetRevision int64, finalStatus StateStatus, diagnostics []string) error {
	if err := g.requirePublished(); err != nil {
		return err
	}
	if finalStatus != StateReady && finalStatus != StateDegraded {
		return fmt.Errorf("invalid completed status %q", finalStatus)
	}

	state, err := g.State()
	if err != nil {
		return fmt.Errorf("read generation state: %w", err)
	}
	if state.Status != StateUpdating || state.TargetRevision == nil || *state.TargetRevision != targetRevision {
		return errors.New("generation update target does not match state")
	}

	state.Revision = targetRevision
	state.TargetRevision = nil
	state.Status = finalStatus
	state.Diagnostics = limitDiagnostics(diagnostics)
	state.UpdatedAt = g.project.now().UTC()

	metadata, err := readGraphMetadata(g.db)
	if err != nil {
		return fmt.Errorf("read graph metadata: %w", err)
	}
	if !graphMetadataMatches(state, metadata) {
		return classifyError(ProblemIncompatible, "completed state and graph metadata mismatch", nil)
	}

	return g.writeState(state)
}

// MarkStatus changes only the usability status of an otherwise committed
// generation. A crash between graph and state writes leaves a marker mismatch,
// which forces recovery on the next open.
func (g *Generation) MarkStatus(status StateStatus, diagnostics []string) error {
	if err := g.requirePublished(); err != nil {
		return err
	}
	if status != StateStale && status != StateFailed {
		return fmt.Errorf("invalid marked status %q", status)
	}

	state, err := g.State()
	if err != nil {
		return fmt.Errorf("read generation state: %w", err)
	}
	if state.Status == StateUpdating || state.TargetRevision != nil {
		return errors.New("cannot mark an interrupted generation")
	}

	if err := updateGraphStatus(g.db, status); err != nil {
		return err
	}

	state.Status = status
	state.Diagnostics = limitDiagnostics(diagnostics)
	state.UpdatedAt = g.project.now().UTC()

	return g.writeState(state)
}

func (g *Generation) requirePublished() error {
	if g == nil || g.project == nil || g.db == nil {
		return errors.New("generation is not open")
	}
	if !g.published {
		return errors.New("generation is not published")
	}

	return nil
}

func (g *Generation) writeState(state State) error {
	if err := validateStateForWrite(state); err != nil {
		return err
	}

	statePath := filepath.Join(g.project.projectDir(), "generations", g.epoch, "state.json")
	return writeStateAtomic(statePath, state)
}

func validateStateForWrite(state State) error {
	if err := validateStateConstants(state); err != nil {
		return err
	}
	if state.Revision < 0 {
		return errors.New("state revision must be non-negative")
	}
	if state.Status == StateUpdating {
		if state.TargetRevision == nil || *state.TargetRevision <= state.Revision {
			return errors.New("updating state requires a target revision greater than the committed revision")
		}
	} else if state.TargetRevision != nil {
		return errors.New("target revision is only valid while updating")
	}

	return nil
}

// Publish atomically makes a fully prepared generation current.
func (g *Generation) Publish(ctx context.Context) error {
	if g == nil || g.project == nil || g.db == nil {
		return errors.New("generation is not open")
	}
	if g.published {
		return errors.New("generation is already published")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	state, err := g.State()
	if err != nil {
		return fmt.Errorf("read generation state: %w", err)
	}
	if err := validateStateConstants(state); err != nil {
		return err
	}
	if state.ProjectID != g.project.projectID || state.ProjectRootFingerprint != g.project.rootFingerprint || state.CanonicalRoot != g.project.canonicalRoot || state.Epoch != g.epoch {
		return classifyError(ProblemIncompatible, "generation identity mismatch", nil)
	}

	metadata, err := readGraphMetadata(g.db)
	if err != nil {
		return classifyError(ProblemCorrupt, "read graph metadata", err)
	}
	if !graphMetadataMatches(state, metadata) {
		return classifyError(ProblemIncompatible, "state and graph metadata mismatch", nil)
	}

	if err := writeCurrentAtomic(filepath.Join(g.project.projectDir(), "CURRENT"), g.epoch); err != nil {
		return err
	}

	g.published = true
	return nil
}

// ReplaceManifest clears and inserts manifest entries in an unpublished generation.
func (g *Generation) ReplaceManifest(value manifest.Manifest) error {
	return g.replaceManifestAndGraph(context.Background(), value, nil, nil)
}

// ReplaceManifestAndGraph prepares all SQLite-derived state for an unpublished generation.
func (g *Generation) ReplaceManifestAndGraph(
	ctx context.Context,
	value manifest.Manifest,
	replacePaths []string,
	files []codegraph.FileGraph,
) error {
	return g.replaceManifestAndGraph(ctx, value, replacePaths, files)
}

func (g *Generation) replaceManifestAndGraph(
	ctx context.Context,
	value manifest.Manifest,
	replacePaths []string,
	files []codegraph.FileGraph,
) error {
	if g == nil || g.db == nil {
		return errors.New("generation is not open")
	}
	if g.published {
		return errors.New("published generation is immutable")
	}

	tx, err := g.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := replaceManifest(tx, value); err != nil {
		return err
	}
	if err := graphstore.New(g.db).Replace(ctx, tx, replacePaths, files); err != nil {
		return fmt.Errorf("replace code graph: %w", err)
	}

	return tx.Commit()
}

func (g *Generation) Manifest() (manifest.Manifest, error) {
	if g == nil || g.db == nil {
		return manifest.Manifest{}, errors.New("generation is not open")
	}

	rows, err := g.db.Query(`SELECT path,size_bytes,mtime_ns,content_hash,language,chunk_count,symbol_count,last_indexed_revision FROM manifest_entries ORDER BY path`)
	if err != nil {
		return manifest.Manifest{}, err
	}
	defer rows.Close()

	result := make([]manifest.Entry, 0)
	for rows.Next() {
		var item manifest.Entry
		if err := rows.Scan(
			&item.Path,
			&item.SizeBytes,
			&item.ModTimeNs,
			&item.ContentHash,
			&item.Language,
			&item.ChunkCount,
			&item.SymbolCount,
			&item.LastIndexedRevision,
		); err != nil {
			return manifest.Manifest{}, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return manifest.Manifest{}, err
	}

	return manifest.Manifest{Files: result}, nil
}

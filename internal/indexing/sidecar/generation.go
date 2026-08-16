package sidecar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

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
	db, err := openGraphDBRead(graphPath)
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

func (g *Generation) State() (State, error) {
	if g == nil || g.project == nil {
		return State{}, errors.New("generation has no project")
	}
	statePath := filepath.Join(g.project.projectDir(), "generations", g.epoch, "state.json")
	return readState(statePath)
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

	if _, err := tx.Exec(`DELETE FROM manifest_entries`); err != nil {
		return err
	}
	statement, err := tx.Prepare(`INSERT INTO manifest_entries(path,size_bytes,mtime_ns,content_hash,language,chunk_count,symbol_count,last_indexed_revision) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer statement.Close()

	files := append([]manifest.Entry(nil), value.Files...)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	for _, file := range files {
		if _, err := statement.Exec(
			file.Path,
			file.SizeBytes,
			file.ModTimeNs,
			file.ContentHash,
			file.Language,
			file.ChunkCount,
			file.SymbolCount,
			file.LastIndexedRevision,
		); err != nil {
			return err
		}
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

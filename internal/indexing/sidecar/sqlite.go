package sidecar

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"s26.dev/istok-cli/internal/codegraph"
	"s26.dev/istok-cli/internal/graphstore"
	"s26.dev/istok-cli/internal/indexing/manifest"
)

func initializeGraph(db *sql.DB, state State) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sidecar_meta (
			key TEXT PRIMARY KEY NOT NULL,
			value TEXT NOT NULL
		)
	`); err != nil {
		return err
	}
	if err := graphstore.Initialize(db); err != nil {
		return fmt.Errorf("initialize code graph: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS manifest_entries (
			path TEXT PRIMARY KEY NOT NULL,
			size_bytes INTEGER NOT NULL,
			mtime_ns INTEGER NOT NULL,
			content_hash TEXT NOT NULL,
			language TEXT NOT NULL,
			chunk_count INTEGER NOT NULL,
			symbol_count INTEGER NOT NULL,
			last_indexed_revision INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}

	updates := map[string]string{
		markerSidecarFormat:          state.SidecarFormat,
		markerGraphSchema:            state.GraphSchema,
		markerProjectID:              state.ProjectID,
		markerProjectRootFingerprint: state.ProjectRootFingerprint,
		markerCanonicalRoot:          state.CanonicalRoot,
		markerEpoch:                  state.Epoch,
		markerRevision:               fmt.Sprintf("%d", state.Revision),
		markerParserSet:              state.ParserSet,
		markerRankingVersion:         state.RankingVersion,
		markerCompatibility:          state.CompatibilityFingerprint,
		markerVersion:                state.Version,
		markerStatus:                 string(state.Status),
	}
	for key, value := range updates {
		if _, err := db.Exec(`INSERT OR REPLACE INTO sidecar_meta(key,value) VALUES(?, ?)`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func readGraphMetadata(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM sidecar_meta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, rows.Err()
}

func graphMetadataMatches(state State, metadata map[string]string) bool {
	required := map[string]string{
		markerSidecarFormat:          SidecarFormat,
		markerGraphSchema:            GraphSchema,
		markerProjectID:              state.ProjectID,
		markerProjectRootFingerprint: state.ProjectRootFingerprint,
		markerCanonicalRoot:          state.CanonicalRoot,
		markerEpoch:                  state.Epoch,
		markerRevision:               fmt.Sprintf("%d", state.Revision),
		markerParserSet:              defaultParserSet,
		markerRankingVersion:         defaultRanking,
		markerCompatibility:          defaultCompatibilityFingerprint,
		markerVersion:                defaultStateVersion,
		markerStatus:                 string(state.Status),
	}
	for key, wanted := range required {
		if metadata[key] != wanted {
			return false
		}
	}
	return true
}

func replaceManifestAndMarkers(db *sql.DB, value manifest.Manifest, revision int64, status StateStatus) error {
	return replaceManifestGraphAndMarkers(context.Background(), db, value, revision, status, nil, nil)
}

func replaceManifestGraphAndMarkers(
	ctx context.Context,
	db *sql.DB,
	value manifest.Manifest,
	revision int64,
	status StateStatus,
	replacePaths []string,
	files []codegraph.FileGraph,
) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := replaceManifest(tx, value); err != nil {
		return err
	}
	if err := graphstore.New(db).Replace(ctx, tx, replacePaths, files); err != nil {
		return fmt.Errorf("replace code graph: %w", err)
	}

	if err := updateRevisionMarkers(tx, revision, status); err != nil {
		return err
	}

	return tx.Commit()
}

func replaceManifest(tx *sql.Tx, value manifest.Manifest) error {
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

	return nil
}

func updateRevisionMarkers(tx *sql.Tx, revision int64, status StateStatus) error {
	for key, value := range map[string]string{
		markerRevision: fmt.Sprintf("%d", revision),
		markerStatus:   string(status),
	} {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO sidecar_meta(key,value) VALUES(?, ?)`, key, value); err != nil {
			return err
		}
	}

	return nil
}

func updateGraphStatus(db *sql.DB, status StateStatus) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO sidecar_meta(key,value) VALUES(?, ?)`, markerStatus, string(status))
	return err
}

func openGraphDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), directoryMode); err != nil {
		return nil, fmt.Errorf("ensure graph directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), directoryMode); err != nil {
		return nil, fmt.Errorf("set graph directory permissions: %w", err)
	}

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_busy_timeout=5000&_foreign_keys=on", filepath.ToSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("open graph database: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open graph database: %w", err)
	}
	if err := os.Chmod(path, fileMode); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set graph database permissions: %w", err)
	}
	return db, nil
}

func openGraphDBRead(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000&_foreign_keys=on", filepath.ToSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("open graph database: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open graph database: %w", err)
	}
	return db, nil
}

const (
	markerSidecarFormat          = "sidecar_format"
	markerGraphSchema            = "graph_schema"
	markerProjectID              = "project_id"
	markerProjectRootFingerprint = "project_root_fingerprint"
	markerCanonicalRoot          = "canonical_root"
	markerEpoch                  = "epoch"
	markerRevision               = "revision"
	markerParserSet              = "parser_set"
	markerRankingVersion         = "ranking_version"
	markerCompatibility          = "compatibility_fingerprint"
	markerVersion                = "version"
	markerStatus                 = "status"
)

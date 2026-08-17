// Package sidecar stores per-project auxiliary metadata and manifest state.
package sidecar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

const (
	directoryMode os.FileMode = 0o700
	fileMode      os.FileMode = 0o600

	SidecarFormat = "istok.index.v1"
	GraphSchema   = "istok.graph.v1"

	defaultParserSet                = "istok.parsers.v2"
	defaultRanking                  = "istok.ranking.v1"
	defaultStateVersion             = "1"
	defaultCompatibilityFingerprint = "parser_set+ranking+schema"
	maxDiagnostics                  = 256
	maxDiagnosticLength             = 512
)

type StateStatus string

const (
	StateNeverIndexed StateStatus = "never_indexed"
	StateUpdating     StateStatus = "updating"
	StateReady        StateStatus = "ready"
	StateStale        StateStatus = "stale"
	StateDegraded     StateStatus = "degraded"
	StateFailed       StateStatus = "failed"
)

type Problem string

const (
	ProblemMissing      Problem = "missing"
	ProblemCorrupt      Problem = "corrupt"
	ProblemIncompatible Problem = "incompatible"
	ProblemRebind       Problem = "rebind"
)

var (
	ErrMissing      = errors.New("sidecar state missing")
	ErrCorrupt      = errors.New("sidecar state corrupt")
	ErrIncompatible = errors.New("sidecar state incompatible")
	ErrRebind       = errors.New("project rebind mismatch")
)

// StateError keeps machine-readable sidecar-state classification.
type StateError struct {
	Problem Problem
	Path    string
	Cause   error
	Message string
}

func (e *StateError) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}
func (e *StateError) Unwrap() error { return e.Cause }

func (e *StateError) Is(target error) bool {
	switch target {
	case ErrMissing:
		return e.Problem == ProblemMissing
	case ErrCorrupt:
		return e.Problem == ProblemCorrupt
	case ErrIncompatible:
		return e.Problem == ProblemIncompatible
	case ErrRebind:
		return e.Problem == ProblemRebind
	default:
		return false
	}
}

// State is persisted in generation/state.json.
type State struct {
	ProjectID                string      `json:"project_id"`
	ProjectRootFingerprint   string      `json:"project_root_fingerprint"`
	CanonicalRoot            string      `json:"canonical_root"`
	Epoch                    string      `json:"epoch"`
	Revision                 int64       `json:"revision"`
	TargetRevision           *int64      `json:"target_revision,omitempty"`
	Status                   StateStatus `json:"status"`
	SidecarFormat            string      `json:"sidecar_format"`
	GraphSchema              string      `json:"graph_schema"`
	ParserSet                string      `json:"parser_set"`
	RankingVersion           string      `json:"ranking_version"`
	CompatibilityFingerprint string      `json:"compatibility_fingerprint"`
	Version                  string      `json:"version"`
	CreatedAt                time.Time   `json:"created_at"`
	UpdatedAt                time.Time   `json:"updated_at"`
	Diagnostics              []string    `json:"diagnostics,omitempty"`
}

type OpenConfig struct {
	ProjectID     string
	CanonicalRoot string
	IndexRoot     string
	Now           func() time.Time
}

func defaultOpenConfig(cfg OpenConfig) OpenConfig {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if strings.TrimSpace(cfg.IndexRoot) == "" {
		cfg.IndexRoot = filepath.Join(xdg.DataHome, "istok", "indexes")
	}
	return cfg
}

// Project owns one sidecar lock for one project UUID.
type Project struct {
	projectID       string
	canonicalRoot   string
	rootFingerprint string
	indexRoot       string
	lock            *flock.Flock
	now             func() time.Time
}

// Open acquires cross-process lock and prepares project directory layout.
func Open(ctx context.Context, cfg OpenConfig) (*Project, error) {
	cfg = defaultOpenConfig(cfg)

	projectID := strings.TrimSpace(cfg.ProjectID)
	if projectID == "" {
		return nil, errors.New("project ID is required")
	}
	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project ID: %w", err)
	}
	root := strings.TrimSpace(cfg.CanonicalRoot)
	if root == "" {
		return nil, errors.New("canonical root is required")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve canonical root: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve canonical root symlinks: %w", err)
	}

	if err := os.MkdirAll(cfg.IndexRoot, directoryMode); err != nil {
		return nil, fmt.Errorf("prepare index root: %w", err)
	}
	if err := os.Chmod(cfg.IndexRoot, directoryMode); err != nil {
		return nil, fmt.Errorf("set index root permissions: %w", err)
	}

	projectRoot := filepath.Join(cfg.IndexRoot, parsedProjectID.String())
	if err := os.MkdirAll(projectRoot, directoryMode); err != nil {
		return nil, fmt.Errorf("prepare project index root: %w", err)
	}
	if err := os.Chmod(projectRoot, directoryMode); err != nil {
		return nil, fmt.Errorf("set project index root permissions: %w", err)
	}

	lockPath := filepath.Join(projectRoot, "LOCK")
	fileLock := flock.New(lockPath, flock.SetPermissions(fileMode))
	ok, err := fileLock.TryLockContext(ctx, 250*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !ok {
		return nil, ctx.Err()
	}

	return &Project{
		projectID:       parsedProjectID.String(),
		canonicalRoot:   canonicalRoot,
		rootFingerprint: sidecarFingerprint(canonicalRoot),
		indexRoot:       cfg.IndexRoot,
		lock:            fileLock,
		now:             cfg.Now,
	}, nil
}

// Close releases lock and closes resources.
func (p *Project) Close() error {
	if p == nil || p.lock == nil {
		return nil
	}
	return p.lock.Close()
}

// Delete removes all indexed data for a project while retaining its stable lock.
func Delete(ctx context.Context, indexRoot, projectID string) error {
	parsedProjectID, err := parseProjectID(projectID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(indexRoot) == "" {
		indexRoot = filepath.Join(xdg.DataHome, "istok", "indexes")
	}

	projectRoot := filepath.Join(indexRoot, parsedProjectID)
	if _, err := os.Stat(projectRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat project sidecar: %w", err)
	}

	fileLock := flock.New(filepath.Join(projectRoot, "LOCK"), flock.SetPermissions(fileMode))
	locked, err := fileLock.TryLockContext(ctx, 250*time.Millisecond)
	if err != nil {
		return fmt.Errorf("acquire delete lock: %w", err)
	}
	if !locked {
		return ctx.Err()
	}
	defer fileLock.Close()

	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return fmt.Errorf("read project sidecar: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == "LOCK" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(projectRoot, entry.Name())); err != nil {
			return fmt.Errorf("remove project sidecar entry %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func parseProjectID(raw string) (string, error) {
	projectID := strings.TrimSpace(raw)
	if projectID == "" {
		return "", errors.New("project ID is required")
	}
	parsed, err := uuid.Parse(projectID)
	if err != nil {
		return "", fmt.Errorf("invalid project ID: %w", err)
	}
	return parsed.String(), nil
}

func ensureExists(path string) error {
	_, err := os.Stat(path)
	return err
}

func validateStateConstants(state State) error {
	if state.SidecarFormat != SidecarFormat {
		return classifyError(ProblemIncompatible, "incompatible sidecar format", nil)
	}
	if state.GraphSchema != GraphSchema {
		return classifyError(ProblemIncompatible, "incompatible graph schema", nil)
	}
	if state.ParserSet != defaultParserSet {
		return classifyError(ProblemIncompatible, "incompatible parser set", nil)
	}
	if state.RankingVersion != defaultRanking {
		return classifyError(ProblemIncompatible, "incompatible ranking version", nil)
	}
	if state.CompatibilityFingerprint != defaultCompatibilityFingerprint {
		return classifyError(ProblemIncompatible, "incompatible compatibility fingerprint", nil)
	}
	if state.Version != defaultStateVersion {
		return classifyError(ProblemIncompatible, "unsupported state version", nil)
	}
	if !validStateStatus(state.Status) {
		return classifyError(ProblemIncompatible, "invalid state status", nil)
	}
	if state.Status == StateUpdating {
		if state.TargetRevision == nil || *state.TargetRevision <= state.Revision {
			return classifyError(ProblemIncompatible, "invalid updating target revision", nil)
		}
	} else if state.TargetRevision != nil {
		return classifyError(ProblemIncompatible, "target revision outside update", nil)
	}
	return nil
}

func validStateStatus(status StateStatus) bool {
	switch status {
	case StateNeverIndexed, StateUpdating, StateReady, StateStale, StateDegraded, StateFailed:
		return true
	default:
		return false
	}
}

func limitDiagnostics(diagnostics []string) []string {
	if len(diagnostics) == 0 {
		return nil
	}

	limit := maxDiagnostics
	if len(diagnostics) < limit {
		limit = len(diagnostics)
	}

	truncated := make([]string, 0, limit)
	for _, diagnostic := range diagnostics[:limit] {
		if len(diagnostic) > maxDiagnosticLength {
			diagnostic = diagnostic[:maxDiagnosticLength]
		}
		truncated = append(truncated, diagnostic)
	}
	return truncated
}

func readState(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}

	var value State
	if err := json.Unmarshal(raw, &value); err != nil {
		return State{}, err
	}
	return value, nil
}

func readCurrent(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func writeCurrentAtomic(path, epoch string) error {
	return writeAtomically(path, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "%s\n", epoch)
		return err
	})
}

func writeStateAtomic(path string, value State) error {
	value.Diagnostics = limitDiagnostics(value.Diagnostics)
	return writeAtomically(path, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	})
}

func writeAtomically(path string, writeFn func(io.Writer) error) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	defer os.Remove(temp.Name())

	if err := temp.Chmod(fileMode); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if err := writeFn(temp); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := syncFile(temp); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("publish atomic file: %w", err)
	}
	return nil
}

func syncFile(file *os.File) error {
	if file == nil {
		return nil
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return nil
}

func classifyError(problem Problem, message string, cause error) error {
	return &StateError{
		Problem: problem,
		Message: message,
		Cause:   cause,
	}
}

func (p *Project) projectDir() string {
	return filepath.Join(p.indexRoot, p.projectID)
}

func sidecarFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func RootFingerprint(path string) string {
	return sidecarFingerprint(filepath.Clean(path))
}

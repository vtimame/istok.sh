// Package artifactstore manages bounded execution artifacts beneath one root.
package artifactstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	directoryMode os.FileMode = 0o700
	fileMode      os.FileMode = 0o600
)

// Store owns a managed artifact root.
type Store struct {
	root string
}

// File describes one committed artifact relative to the managed root.
type File struct {
	RelativePath string
	SHA256       string
	OriginalSize int64
	StoredSize   int64
	Truncated    bool
	MediaType    string
}

// New creates or opens a managed artifact root.
func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("artifactstore: root is required")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("artifactstore: resolve root: %w", err)
	}
	if err := os.MkdirAll(absRoot, directoryMode); err != nil {
		return nil, fmt.Errorf("artifactstore: create root: %w", err)
	}
	if err := os.Chmod(absRoot, directoryMode); err != nil {
		return nil, fmt.Errorf("artifactstore: set root permissions: %w", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("artifactstore: resolve root symlinks: %w", err)
	}

	return &Store{root: resolvedRoot}, nil
}

// NewCapture starts a bounded capture for one execution artifact.
func (store *Store) NewCapture(runID, executionID, kind string, limit int64) (*Capture, error) {
	if store == nil {
		return nil, errors.New("artifactstore: nil store")
	}
	if limit < 0 {
		return nil, errors.New("artifactstore: limit must not be negative")
	}
	for _, value := range []string{runID, executionID, kind} {
		if !safePathPart(value) {
			return nil, fmt.Errorf("artifactstore: unsafe path component %q", value)
		}
	}

	relativePath := filepath.Join("runs", runID, "executions", executionID, kind+".log")
	finalPath, err := store.resolve(relativePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), directoryMode); err != nil {
		return nil, fmt.Errorf("artifactstore: create artifact directory: %w", err)
	}
	resolvedDirectory, err := filepath.EvalSymlinks(filepath.Dir(finalPath))
	if err != nil {
		return nil, fmt.Errorf("artifactstore: resolve artifact directory: %w", err)
	}
	if !contained(store.root, resolvedDirectory) {
		return nil, errors.New("artifactstore: artifact directory escapes managed root")
	}
	finalPath = filepath.Join(resolvedDirectory, filepath.Base(finalPath))
	if err := os.Chmod(resolvedDirectory, directoryMode); err != nil {
		return nil, fmt.Errorf("artifactstore: set artifact directory permissions: %w", err)
	}

	temporary, err := os.CreateTemp(resolvedDirectory, ".artifact-*")
	if err != nil {
		return nil, fmt.Errorf("artifactstore: create temporary artifact: %w", err)
	}
	if err := temporary.Chmod(fileMode); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return nil, fmt.Errorf("artifactstore: set temporary artifact permissions: %w", err)
	}

	return &Capture{
		file:         temporary,
		finalPath:    finalPath,
		relativePath: filepath.ToSlash(relativePath),
		limit:        limit,
		hash:         sha256.New(),
	}, nil
}

// Verify checks that file remains contained beneath the store root and still
// has its declared stored size and SHA-256 hash.
func (store *Store) Verify(file File) error {
	if store == nil {
		return errors.New("artifactstore: nil store")
	}
	path, err := store.resolve(file.RelativePath)
	if err != nil {
		return err
	}

	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("artifactstore: resolve artifact: %w", err)
	}
	if !contained(store.root, resolvedPath) {
		return errors.New("artifactstore: artifact escapes managed root")
	}

	handle, err := os.Open(resolvedPath)
	if err != nil {
		return fmt.Errorf("artifactstore: open artifact: %w", err)
	}
	defer handle.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, handle)
	if err != nil {
		return fmt.Errorf("artifactstore: hash artifact: %w", err)
	}
	if size != file.StoredSize {
		return fmt.Errorf("artifactstore: stored size mismatch: got %d, want %d", size, file.StoredSize)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != file.SHA256 {
		return errors.New("artifactstore: SHA-256 mismatch")
	}

	return nil
}

// Remove deletes a committed artifact after a persistence failure. It only
// accepts paths that resolve beneath the managed root.
func (store *Store) Remove(file File) error {
	if store == nil {
		return errors.New("artifactstore: nil store")
	}

	path, err := store.resolve(file.RelativePath)
	if err != nil {
		return err
	}
	resolvedDirectory, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("artifactstore: resolve artifact directory: %w", err)
	}
	if !contained(store.root, resolvedDirectory) {
		return errors.New("artifactstore: artifact directory escapes managed root")
	}
	path = filepath.Join(resolvedDirectory, filepath.Base(path))
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("artifactstore: remove artifact: %w", err)
	}

	return nil
}

func (store *Store) resolve(relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) || !safeRelativePath(relativePath) {
		return "", errors.New("artifactstore: path escapes managed root")
	}

	path := filepath.Join(store.root, filepath.FromSlash(relativePath))
	if !contained(store.root, path) {
		return "", errors.New("artifactstore: path escapes managed root")
	}

	return path, nil
}

func safeRelativePath(path string) bool {
	if path == "" || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if !safePathPart(part) {
			return false
		}
	}
	return true
}

func safePathPart(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	for _, character := range part {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

func contained(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// Capture is a streaming artifact writer. Commit or Abort must be called when
// writing is complete.
type Capture struct {
	file         *os.File
	finalPath    string
	relativePath string
	limit        int64
	hash         hash.Hash
	sample       []byte
	originalSize int64
	storedSize   int64
	closed       bool
	committed    bool
}

// Write stores at most the configured limit while accounting for all input.
func (capture *Capture) Write(data []byte) (int, error) {
	if capture == nil || capture.closed {
		return 0, os.ErrClosed
	}

	capture.originalSize += int64(len(data))
	remaining := capture.limit - capture.storedSize
	if remaining <= 0 {
		return len(data), nil
	}
	toStore := data
	if int64(len(toStore)) > remaining {
		toStore = toStore[:remaining]
	}

	written, err := capture.file.Write(toStore)
	if written > 0 {
		stored := toStore[:written]
		_, _ = capture.hash.Write(stored)
		capture.storedSize += int64(written)
		if len(capture.sample) < 512 {
			remainingSample := 512 - len(capture.sample)
			if len(stored) > remainingSample {
				stored = stored[:remainingSample]
			}
			capture.sample = append(capture.sample, stored...)
		}
	}
	if err != nil {
		return written, fmt.Errorf("artifactstore: write artifact: %w", err)
	}
	if written != len(toStore) {
		return written, io.ErrShortWrite
	}

	return len(data), nil
}

// Commit atomically publishes the artifact and returns its metadata.
func (capture *Capture) Commit() (File, error) {
	if capture == nil || capture.closed {
		return File{}, os.ErrClosed
	}
	if err := capture.file.Sync(); err != nil {
		_ = capture.Abort()
		return File{}, fmt.Errorf("artifactstore: sync artifact: %w", err)
	}
	if err := capture.file.Close(); err != nil {
		capture.closed = true
		_ = os.Remove(capture.file.Name())
		return File{}, fmt.Errorf("artifactstore: close artifact: %w", err)
	}
	capture.closed = true

	if err := os.Chmod(capture.file.Name(), fileMode); err != nil {
		_ = os.Remove(capture.file.Name())
		return File{}, fmt.Errorf("artifactstore: set artifact permissions: %w", err)
	}
	if _, err := os.Lstat(capture.finalPath); err == nil {
		_ = os.Remove(capture.file.Name())
		return File{}, errors.New("artifactstore: artifact already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(capture.file.Name())
		return File{}, fmt.Errorf("artifactstore: inspect artifact target: %w", err)
	}
	if err := os.Rename(capture.file.Name(), capture.finalPath); err != nil {
		_ = os.Remove(capture.file.Name())
		return File{}, fmt.Errorf("artifactstore: publish artifact: %w", err)
	}
	capture.committed = true

	return File{
		RelativePath: capture.relativePath,
		SHA256:       hex.EncodeToString(capture.hash.Sum(nil)),
		OriginalSize: capture.originalSize,
		StoredSize:   capture.storedSize,
		Truncated:    capture.originalSize > capture.storedSize,
		MediaType:    http.DetectContentType(capture.sample),
	}, nil
}

// Abort discards the temporary artifact. It is safe to call more than once.
func (capture *Capture) Abort() error {
	if capture == nil || capture.committed {
		return nil
	}
	if !capture.closed {
		if err := capture.file.Close(); err != nil {
			return fmt.Errorf("artifactstore: close temporary artifact: %w", err)
		}
		capture.closed = true
	}
	if err := os.Remove(capture.file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("artifactstore: remove temporary artifact: %w", err)
	}
	return nil
}

package artifactstore

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureTruncatesAndVerifies(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	capture, err := store.NewCapture("run-1", "execution-1", "stdout", 5)
	if err != nil {
		t.Fatalf("NewCapture() error = %v", err)
	}
	if _, err := capture.Write([]byte("abcdefgh")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	file, err := capture.Commit()
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte("abcde")))
	if file.OriginalSize != 8 || file.StoredSize != 5 || !file.Truncated || file.SHA256 != wantHash {
		t.Errorf("File = %+v", file)
	}
	if filepath.IsAbs(file.RelativePath) || strings.Contains(file.RelativePath, "..") {
		t.Errorf("RelativePath = %q is unsafe", file.RelativePath)
	}
	if err := store.Verify(file); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(store.root, filepath.FromSlash(file.RelativePath)))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != fileMode {
		t.Errorf("file permissions = %o, want %o", info.Mode().Perm(), fileMode)
	}
	for _, path := range []string{
		store.root,
		filepath.Join(store.root, "runs"),
		filepath.Join(store.root, "runs", "run-1", "executions", "execution-1"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
		if info.Mode().Perm() != directoryMode {
			t.Errorf("directory permissions for %q = %o, want %o", path, info.Mode().Perm(), directoryMode)
		}
	}
}

func TestCaptureEmptyArtifact(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	capture, err := store.NewCapture("run-1", "execution-1", "stderr", 0)
	if err != nil {
		t.Fatalf("NewCapture() error = %v", err)
	}
	file, err := capture.Commit()
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if file.OriginalSize != 0 || file.StoredSize != 0 || file.Truncated {
		t.Errorf("File = %+v", file)
	}
	if err := store.Verify(file); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsTamperingAndUnsafePaths(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	capture, err := store.NewCapture("run-1", "execution-1", "stdout", 32)
	if err != nil {
		t.Fatalf("NewCapture() error = %v", err)
	}
	_, _ = capture.Write([]byte("original"))
	file, err := capture.Commit()
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	path := filepath.Join(store.root, filepath.FromSlash(file.RelativePath))
	if err := os.WriteFile(path, []byte("tampered"), fileMode); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := store.Verify(file); err == nil {
		t.Fatal("Verify() error = nil after tampering")
	}
	if err := store.Verify(File{RelativePath: "../outside", StoredSize: 0}); err == nil {
		t.Fatal("Verify() error = nil for traversal path")
	}
	if _, err := store.NewCapture("../run", "execution", "stdout", 1); err == nil {
		t.Fatal("NewCapture() error = nil for traversal component")
	}
}

func TestStoreRemoveCommittedArtifact(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	capture, err := store.NewCapture("run", "execution", "stdout", 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capture.Write([]byte("output")); err != nil {
		t.Fatal(err)
	}
	file, err := capture.Commit()
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(file); err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
	if err := store.Verify(file); err == nil {
		t.Fatal("Verify() error = nil after Remove")
	}
}

func TestNewCaptureRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "runs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := store.NewCapture("run", "execution", "stdout", 16); err == nil {
		t.Fatal("NewCapture() error = nil for symlink escape")
	}
}

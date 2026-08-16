package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

type captureRepository struct {
	value Project
	root  Root
}

func (r *captureRepository) Init(_ context.Context, value Project, root Root) (InitResult, error) {
	r.value = value
	r.root = root
	return InitResult{Project: value, Created: true}, nil
}
func (r *captureRepository) List(context.Context, bool) ([]Project, error)    { return nil, nil }
func (r *captureRepository) Current(context.Context, string) (Project, error) { return Project{}, nil }
func (r *captureRepository) Resolve(context.Context, Selector, bool) (Project, error) {
	return Project{}, nil
}
func (r *captureRepository) Rename(context.Context, string, string, *int64) (Project, error) {
	return Project{}, nil
}
func (r *captureRepository) Rebind(context.Context, string, Root, *int64) (Project, error) {
	return Project{}, nil
}
func (r *captureRepository) Delete(context.Context, string, *int64) (Project, error) {
	return Project{}, nil
}
func (r *captureRepository) Restore(context.Context, string, *Root, *int64) (Project, error) {
	return Project{}, nil
}

func TestInitUsesUUIDv7AndCanonicalBasename(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "Пространство name")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	repository := &captureRepository{}
	result, err := NewService(repository).Init(context.Background(), directory, "")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := uuid.Parse(result.Project.ID)
	if err != nil || parsed.Version() != 7 {
		t.Fatalf("ID = %q, want UUIDv7", result.Project.ID)
	}
	if repository.value.Name != "Пространство name" {
		t.Fatalf("name = %q", repository.value.Name)
	}
}

func TestCanonicalizeResolvesSymlink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(target), "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root, err := Canonicalize(alias)
	if err != nil {
		t.Fatal(err)
	}
	if root.CanonicalPath != target {
		t.Fatalf("canonical path = %q, want %q", root.CanonicalPath, target)
	}
}

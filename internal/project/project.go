// Package project contains the application API for local Istok projects.
package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Code string

const (
	CodeInvalid          Code = "invalid_argument"
	CodeNotFound         Code = "project_not_found"
	CodeAmbiguous        Code = "project_ambiguous"
	CodeConflict         Code = "project_conflict"
	CodeRevisionConflict Code = "revision_conflict"
	CodeInternal         Code = "internal_error"
)

type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

func code(code Code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func ErrorCode(err error) Code {
	var value *Error
	if errors.As(err, &value) {
		return value.Code
	}
	return CodeInternal
}

type Root struct {
	CanonicalPath string     `json:"canonical_path"`
	PathKey       string     `json:"-"`
	ActiveAt      time.Time  `json:"active_at"`
	DetachedAt    *time.Time `json:"detached_at,omitempty"`
}

type Project struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Revision  int64      `json:"revision"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Root      *Root      `json:"root,omitempty"`
}

type InitResult struct {
	Project Project `json:"project"`
	Created bool    `json:"created"`
}

type Selector string

type Repository interface {
	Init(context.Context, Project, Root) (InitResult, error)
	List(context.Context, bool) ([]Project, error)
	Current(context.Context, string) (Project, error)
	Resolve(context.Context, Selector, bool) (Project, error)
	Rename(context.Context, string, string, *int64) (Project, error)
	Rebind(context.Context, string, Root, *int64) (Project, error)
	Delete(context.Context, string, *int64) (Project, error)
	Restore(context.Context, string, *Root, *int64) (Project, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func Canonicalize(path string) (Root, error) {
	if strings.TrimSpace(path) == "" {
		return Root{}, code(CodeInvalid, "project path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Root{}, fmt.Errorf("make project path absolute: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return Root{}, code(CodeInvalid, "resolve project path: %v", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Root{}, code(CodeInvalid, "stat project path: %v", err)
	}
	if !info.IsDir() {
		return Root{}, code(CodeInvalid, "project path is not a directory: %s", resolved)
	}
	key := filepath.Clean(resolved)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return Root{CanonicalPath: resolved, PathKey: key}, nil
}

func (s *Service) Init(ctx context.Context, path, name string) (InitResult, error) {
	root, err := Canonicalize(path)
	if err != nil {
		return InitResult{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(root.CanonicalPath)
	}
	name = strings.TrimSpace(name)
	id, err := uuid.NewV7()
	if err != nil {
		return InitResult{}, fmt.Errorf("generate project ID: %w", err)
	}
	now := s.now().UTC()
	root.ActiveAt = now

	return s.repository.Init(ctx, Project{ID: id.String(), Name: name, Revision: 1, CreatedAt: now, UpdatedAt: now}, root)
}

func (s *Service) List(ctx context.Context, deleted bool) ([]Project, error) {
	return s.repository.List(ctx, deleted)
}

func (s *Service) Current(ctx context.Context, cwd string) (Project, error) {
	root, err := Canonicalize(cwd)
	if err != nil {
		return Project{}, err
	}

	return s.repository.Current(ctx, root.PathKey)
}

func (s *Service) Resolve(ctx context.Context, selector string, deleted bool) (Project, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return Project{}, code(CodeInvalid, "project selector is required")
	}

	return s.repository.Resolve(ctx, Selector(selector), deleted)
}

func (s *Service) Rename(ctx context.Context, selector, name string, expected *int64) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, code(CodeInvalid, "project name is required")
	}

	p, err := s.Resolve(ctx, selector, true)
	if err != nil {
		return Project{}, err
	}

	return s.repository.Rename(ctx, p.ID, name, expected)
}

func (s *Service) Rebind(ctx context.Context, selector, path string, expected *int64) (Project, error) {
	root, err := Canonicalize(path)
	if err != nil {
		return Project{}, err
	}
	p, err := s.Resolve(ctx, selector, false)
	if err != nil {
		return Project{}, err
	}

	root.ActiveAt = s.now().UTC()

	return s.repository.Rebind(ctx, p.ID, root, expected)
}

func (s *Service) Delete(ctx context.Context, selector string, expected *int64) (Project, error) {
	p, err := s.Resolve(ctx, selector, true)
	if err != nil {
		return Project{}, err
	}

	return s.repository.Delete(ctx, p.ID, expected)
}

func (s *Service) Restore(ctx context.Context, selector, path string, expected *int64) (Project, error) {
	p, err := s.Resolve(ctx, selector, true)
	if err != nil {
		return Project{}, err
	}

	var root *Root
	if path != "" {
		v, err := Canonicalize(path)
		if err != nil {
			return Project{}, err
		}
		v.ActiveAt = s.now().UTC()
		root = &v
	} else if p.DeletedAt != nil {
		if p.Root == nil {
			return Project{}, code(CodeConflict, "project has no root to restore")
		}

		v, err := Canonicalize(p.Root.CanonicalPath)
		if err != nil {
			return Project{}, code(CodeConflict, "previous project root is unavailable; provide a path")
		}

		v.ActiveAt = s.now().UTC()
		root = &v
	}

	return s.repository.Restore(ctx, p.ID, root, expected)
}

package indexstore

import (
	"errors"
	"fmt"
)

type Problem string

const (
	ProblemMissing          Problem = "missing"
	ProblemCorrupt          Problem = "corrupt"
	ProblemIncompatible     Problem = "incompatible"
	ProblemRevisionMismatch Problem = "revision_mismatch"
)

var (
	ErrMissing          = errors.New("index store missing")
	ErrCorrupt          = errors.New("index store corrupt")
	ErrIncompatible     = errors.New("index store incompatible")
	ErrRevisionMismatch = errors.New("index store revision mismatch")
)

type Error struct {
	Problem Problem
	Path    string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("index store %s at %s: %v", e.Problem, e.Path, e.Cause)
	}
	return fmt.Sprintf("index store %s at %s", e.Problem, e.Path)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func (e *Error) Is(target error) bool {
	switch target {
	case ErrMissing:
		return e.Problem == ProblemMissing
	case ErrCorrupt:
		return e.Problem == ProblemCorrupt
	case ErrIncompatible:
		return e.Problem == ProblemIncompatible
	case ErrRevisionMismatch:
		return e.Problem == ProblemRevisionMismatch
	default:
		return false
	}
}

func missing(path string) error { return &Error{Problem: ProblemMissing, Path: path} }
func corrupt(path string, err error) error {
	return &Error{Problem: ProblemCorrupt, Path: path, Cause: err}
}
func incompatible(path string, err error) error {
	return &Error{Problem: ProblemIncompatible, Path: path, Cause: err}
}
func revisionMismatch(path string, err error) error {
	return &Error{Problem: ProblemRevisionMismatch, Path: path, Cause: err}
}

// Package indexing coordinates discovery, manifests, and the lexical sidecar.
package indexing

import (
	"errors"
	"fmt"

	"s26.dev/istok-cli/internal/codegraph"
	"s26.dev/istok-cli/internal/indexing/sidecar"
)

const ErrorCode = "index_failed"

type Config struct {
	IndexRoot string
}

type Status struct {
	State          sidecar.StateStatus `json:"state"`
	EpochID        string              `json:"epoch_id,omitempty"`
	Revision       int64               `json:"revision"`
	TargetRevision *int64              `json:"target_revision,omitempty"`
	Diagnostics    []string            `json:"diagnostics,omitempty"`
}

const (
	maxSearchLimit    = 500
	defaultGraphLimit = 50
	defaultPathLimit  = 10
	maxGraphLimit     = 500
)

type SymbolRequest struct {
	Name  string
	Limit int
}

type NeighborsRequest struct {
	Name  string
	Kinds []codegraph.EdgeKind
	Limit int
}

type GraphNeighbor struct {
	Source       codegraph.Node       `json:"source"`
	Target       codegraph.Node       `json:"target"`
	Kind         codegraph.EdgeKind   `json:"kind"`
	Provenance   codegraph.Provenance `json:"provenance"`
	Confidence   float64              `json:"confidence"`
	EvidencePath string               `json:"evidence_path"`
	EvidenceLine int                  `json:"evidence_line"`
}

type PathRequest struct {
	From     string
	To       string
	MaxDepth int
	Limit    int
}

type GraphPath struct {
	Nodes []codegraph.Node `json:"nodes"`
}

type Error struct {
	Status Status
	Cause  error
}

func (e *Error) Error() string {
	if e == nil || e.Cause == nil {
		return "index operation failed"
	}

	return fmt.Sprintf("index operation failed: %v", e.Cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

func IsError(err error) bool {
	var value *Error
	return errors.As(err, &value)
}

func statusFromState(state sidecar.State) Status {
	return Status{
		State:          state.Status,
		EpochID:        state.Epoch,
		Revision:       state.Revision,
		TargetRevision: cloneRevision(state.TargetRevision),
		Diagnostics:    append([]string(nil), state.Diagnostics...),
	}
}

func failedStatus(err error) Status {
	return Status{
		State:       sidecar.StateFailed,
		Diagnostics: []string{err.Error()},
	}
}

func wrapError(status Status, err error) error {
	if err == nil {
		return nil
	}

	return &Error{Status: status, Cause: err}
}

func cloneRevision(value *int64) *int64 {
	if value == nil {
		return nil
	}

	copy := *value
	return &copy
}

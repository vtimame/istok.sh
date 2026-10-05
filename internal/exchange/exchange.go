// Package exchange defines the bundle that moves projects between devices and
// the report an import produces. A bundle carries rows of the local database
// tables that are worth sharing; everything bound to one machine (project
// paths, artifact files, indexes, retrieved code snippets) stays behind.
package exchange

import (
	"errors"
	"fmt"
	"time"
)

// Format names the bundle layout. Readers reject any other value.
const Format = "istok.export.v1"

// Row is one database row keyed by column name. Values are JSON scalars:
// strings, numbers, booleans or null.
type Row map[string]any

// Bundle is the exported file. SchemaVersion is the database migration
// version of the exporting device; an import requires the same version, so
// both devices must run the same Istok release.
type Bundle struct {
	Format        string           `json:"format"`
	SchemaVersion int64            `json:"schema_version"`
	IstokVersion  string           `json:"istok_version"`
	ExportedAt    time.Time        `json:"exported_at"`
	Projects      []ProjectSummary `json:"projects"`
	Tables        map[string][]Row `json:"tables"`
}

// ProjectSummary lets a reader see what a bundle contains without parsing rows.
type ProjectSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Tasks int    `json:"tasks"`
}

// Validate checks the envelope before any row is read.
func (b Bundle) Validate(schemaVersion int64) error {
	if b.Format != Format {
		return Errorf(CodeInvalid, "unsupported bundle format %q; expected %q", b.Format, Format)
	}
	if b.SchemaVersion != schemaVersion {
		return Errorf(CodeIncompatible,
			"bundle was exported with database schema %d, this Istok uses %d; update Istok on both devices to the same version",
			b.SchemaVersion, schemaVersion)
	}
	if len(b.Projects) == 0 {
		return Errorf(CodeInvalid, "bundle contains no projects")
	}

	return nil
}

// Report describes what an import did, or would do in a dry run.
type Report struct {
	DryRun     bool                   `json:"dry_run"`
	Projects   []ProjectOutcome       `json:"projects"`
	Tables     map[string]TableCounts `json:"tables"`
	Renumbered []Renumbering          `json:"renumbered"`
	Conflicts  []Conflict             `json:"conflicts"`
	Warnings   []string               `json:"warnings"`
}

// ProjectOutcome reports one project of the bundle.
type ProjectOutcome struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"created"`

	// Bound is false when the project has no folder on this device yet.
	Bound bool `json:"bound"`
}

// TableCounts counts rows per outcome for one table.
type TableCounts struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	KeptLocal int `json:"kept_local"`
	Skipped   int `json:"skipped"`
}

// Renumbering records a task that received a new number because another task
// already used its number on this device.
type Renumbering struct {
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	From      int64  `json:"from"`
	To        int64  `json:"to"`
}

// Conflict records a row the import could not apply. The local row is kept.
type Conflict struct {
	Table  string `json:"table"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// NewReport returns an empty report with non-nil collections, so JSON output
// never contains null lists.
func NewReport(dryRun bool) Report {
	return Report{
		DryRun:     dryRun,
		Projects:   []ProjectOutcome{},
		Tables:     map[string]TableCounts{},
		Renumbered: []Renumbering{},
		Conflicts:  []Conflict{},
		Warnings:   []string{},
	}
}

// Code classifies exchange errors for transports.
type Code string

const (
	CodeInvalid      Code = "invalid_argument"
	CodeIncompatible Code = "incompatible_bundle"
	CodeNotFound     Code = "not_found"
)

// Error is a typed exchange error.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

// Errorf builds a typed exchange error.
func Errorf(code Code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrorCode returns the code of a typed exchange error.
func ErrorCode(err error) (Code, bool) {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code, true
	}

	return "", false
}

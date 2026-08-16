package run

import (
	"encoding/hex"
	"strings"
	"time"

	projectcontext "s26.dev/istok-cli/internal/context"
)

const ContextSnapshotSchemaVersion = "1"

type ContextSnapshot struct {
	ID            string                `json:"id"`
	SchemaVersion string                `json:"schema_version"`
	ProjectID     string                `json:"project_id"`
	GeneratedAt   time.Time             `json:"generated_at"`
	CreatedAt     time.Time             `json:"created_at"`
	Records       []ContextSnapshotItem `json:"records"`
}

type ContextSnapshotItem struct {
	RecordID       string                     `json:"record_id"`
	RecordRevision int64                      `json:"record_revision"`
	ContentHash    string                     `json:"content_hash"`
	Kind           projectcontext.Kind        `json:"kind"`
	Source         projectcontext.Source      `json:"source"`
	Visibility     projectcontext.Visibility  `json:"visibility"`
	Sensitivity    projectcontext.Sensitivity `json:"sensitivity"`
	Title          string                     `json:"title"`
	Body           string                     `json:"body"`
	Snippet        string                     `json:"snippet"`
	Tags           []string                   `json:"tags"`
}

func (v ContextSnapshot) Validate() error {
	if !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "context snapshot id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.ProjectID) {
		return NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if v.SchemaVersion != ContextSnapshotSchemaVersion {
		return NewError(CodeInvalid, "invalid context snapshot schema version")
	}
	if v.GeneratedAt.IsZero() {
		return NewError(CodeInvalid, "generated at is required")
	}
	for _, value := range v.Records {
		if err := value.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func NewContextSnapshot(snapshotID string, value projectcontext.ContextPackage, projectID string) (ContextSnapshot, error) {
	if snapshotID == "" || !IsUUIDv7(snapshotID) {
		return ContextSnapshot{}, NewError(CodeInvalid, "context snapshot id must be a canonical UUIDv7")
	}
	if projectID == "" || !IsUUIDv7(projectID) {
		return ContextSnapshot{}, NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if value.SchemaVersion != ContextSnapshotSchemaVersion {
		return ContextSnapshot{}, NewError(CodeInvalid, "invalid context snapshot schema version")
	}
	if value.ProjectID == "" || value.ProjectID != projectID {
		return ContextSnapshot{}, NewError(CodeInvalid, "context package project id mismatch")
	}
	if value.GeneratedAt.IsZero() {
		return ContextSnapshot{}, NewError(CodeInvalid, "generated at is required")
	}

	records := make([]ContextSnapshotItem, len(value.Records))
	for i := range value.Records {
		record := value.Records[i]
		tags := make([]string, len(record.Tags))
		copy(tags, record.Tags)

		item := ContextSnapshotItem{
			RecordID:       record.RecordID,
			RecordRevision: record.RecordRevision,
			ContentHash:    record.ContentHash,
			Kind:           record.Kind,
			Source:         record.Source,
			Visibility:     record.Visibility,
			Sensitivity:    record.Sensitivity,
			Title:          record.Title,
			Body:           record.Body,
			Snippet:        record.Snippet,
			Tags:           tags,
		}

		if err := item.Validate(); err != nil {
			return ContextSnapshot{}, err
		}

		records[i] = item
	}

	return ContextSnapshot{
		ID:            snapshotID,
		SchemaVersion: ContextSnapshotSchemaVersion,
		ProjectID:     projectID,
		GeneratedAt:   value.GeneratedAt,
		CreatedAt:     time.Now(),
		Records:       records,
	}, nil
}

func (v ContextSnapshotItem) Validate() error {
	if v.RecordID == "" || v.Title == "" {
		return NewError(CodeInvalid, "context snapshot item is invalid")
	}
	if !IsUUIDv7(v.RecordID) {
		return NewError(CodeInvalid, "context snapshot item record id must be a canonical UUIDv7")
	}
	if v.RecordRevision < 1 {
		return NewError(CodeInvalid, "context snapshot item revision must be >= 1")
	}
	if len(v.ContentHash) != 64 {
		return NewError(CodeInvalid, "context snapshot item content hash must be a 64-character hex string")
	}
	if _, err := hex.DecodeString(v.ContentHash); err != nil {
		return NewError(CodeInvalid, "context snapshot item content hash must be a 64-character hex string")
	}
	if !v.Kind.Valid() {
		return NewError(CodeInvalid, "context snapshot item kind is invalid")
	}
	if !v.Source.Valid() {
		return NewError(CodeInvalid, "context snapshot item source is invalid")
	}
	if !v.Visibility.Valid() {
		return NewError(CodeInvalid, "context snapshot item visibility is invalid")
	}
	if !v.Sensitivity.Valid() {
		return NewError(CodeInvalid, "context snapshot item sensitivity is invalid")
	}
	if strings.TrimSpace(v.Title) == "" {
		return NewError(CodeInvalid, "context snapshot item title is required")
	}

	for _, value := range v.Tags {
		if strings.TrimSpace(value) == "" {
			return NewError(CodeInvalid, "context snapshot item tag must not be blank")
		}
	}

	return nil
}

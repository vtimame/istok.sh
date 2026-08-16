package context

import (
	"encoding/json"
	"strings"
	"time"
)

type Kind string

const (
	KindNote        Kind = "note"
	KindDecision    Kind = "decision"
	KindInstruction Kind = "instruction"
	KindConstraint  Kind = "constraint"
)

func (v Kind) Valid() bool {
	return v == KindNote || v == KindDecision || v == KindInstruction || v == KindConstraint
}

type Source string

const (
	SourceUser   Source = "user"
	SourceAgent  Source = "agent"
	SourceImport Source = "import"
)

func (v Source) Valid() bool {
	return v == SourceUser || v == SourceAgent || v == SourceImport
}

type Visibility string

const (
	VisibilityShared    Visibility = "shared"
	VisibilityLocalOnly Visibility = "local_only"
)

func (v Visibility) Valid() bool {
	return v == VisibilityShared || v == VisibilityLocalOnly
}

type Sensitivity string

const (
	SensitivityNormal  Sensitivity = "normal"
	SensitivityPrivate Sensitivity = "private"
)

func (v Sensitivity) Valid() bool {
	return v == SensitivityNormal || v == SensitivityPrivate
}

type ActorSnapshot struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (a ActorSnapshot) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Kind) == "" || strings.TrimSpace(a.Name) == "" {
		return NewError(CodeInvalid, "actor id, kind, and name are required")
	}

	return nil
}

type ProjectContextRecord struct {
	ID          string        `json:"id"`
	ProjectID   string        `json:"project_id"`
	Revision    int64         `json:"revision"`
	Kind        Kind          `json:"kind"`
	Title       string        `json:"title"`
	Body        string        `json:"body"`
	Tags        []string      `json:"tags"`
	Source      Source        `json:"source"`
	Visibility  Visibility    `json:"visibility"`
	Sensitivity Sensitivity   `json:"sensitivity"`
	Actor       ActorSnapshot `json:"actor"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	DeletedAt   *time.Time    `json:"deleted_at,omitempty"`
}

type ContextEvent struct {
	ID             string        `json:"id"`
	RecordID       string        `json:"record_id"`
	Type           string        `json:"type"`
	Body           string        `json:"body"`
	RecordRevision int64         `json:"record_revision"`
	Actor          ActorSnapshot `json:"actor"`
	CreatedAt      time.Time     `json:"created_at"`
}

type CreateInput struct {
	ID          string      `json:"id,omitempty"`
	ProjectID   string      `json:"project_id"`
	Kind        Kind        `json:"kind"`
	Title       string      `json:"title"`
	Body        string      `json:"body"`
	Tags        []string    `json:"tags"`
	Source      Source      `json:"source"`
	Visibility  Visibility  `json:"visibility"`
	Sensitivity Sensitivity `json:"sensitivity"`
}

func (v CreateInput) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "context record id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.ProjectID) {
		return NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if !v.Kind.Valid() {
		return NewError(CodeInvalid, "context kind is invalid")
	}
	if strings.TrimSpace(v.Title) == "" {
		return NewError(CodeInvalid, "context title is required")
	}
	if !v.Source.Valid() {
		return NewError(CodeInvalid, "context source is invalid")
	}
	if !v.Visibility.Valid() {
		return NewError(CodeInvalid, "context visibility is invalid")
	}
	if !v.Sensitivity.Valid() {
		return NewError(CodeInvalid, "context sensitivity is invalid")
	}

	if _, err := NormalizeTags(v.Tags); err != nil {
		return err
	}

	return nil
}

type Patch struct {
	Kind        *Kind        `json:"kind,omitempty"`
	Title       *string      `json:"title,omitempty"`
	Body        *string      `json:"body,omitempty"`
	Tags        *[]string    `json:"tags,omitempty"`
	Source      *Source      `json:"source,omitempty"`
	Visibility  *Visibility  `json:"visibility,omitempty"`
	Sensitivity *Sensitivity `json:"sensitivity,omitempty"`
}

func (v Patch) Validate() error {
	if v.Kind == nil && v.Title == nil && v.Body == nil && v.Tags == nil && v.Source == nil && v.Visibility == nil && v.Sensitivity == nil {
		return NewError(CodeInvalid, "context patch must not be empty")
	}

	if v.Title != nil && strings.TrimSpace(*v.Title) == "" {
		return NewError(CodeInvalid, "context title must not be blank")
	}
	if v.Kind != nil && !v.Kind.Valid() {
		return NewError(CodeInvalid, "context kind is invalid")
	}
	if v.Source != nil && !v.Source.Valid() {
		return NewError(CodeInvalid, "context source is invalid")
	}
	if v.Visibility != nil && !v.Visibility.Valid() {
		return NewError(CodeInvalid, "context visibility is invalid")
	}
	if v.Sensitivity != nil && !v.Sensitivity.Valid() {
		return NewError(CodeInvalid, "context sensitivity is invalid")
	}
	if v.Tags != nil {
		if _, err := NormalizeTags(*v.Tags); err != nil {
			return err
		}
	}

	return nil
}

func (v Patch) Apply(value *ProjectContextRecord) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if value.DeletedAt != nil {
		return NewError(CodeConflict, "cannot mutate a deleted context record")
	}

	if v.Kind != nil {
		value.Kind = *v.Kind
	}
	if v.Title != nil {
		value.Title = strings.TrimSpace(*v.Title)
	}
	if v.Body != nil {
		value.Body = *v.Body
	}
	if v.Tags != nil {
		tags, _ := NormalizeTags(*v.Tags)
		value.Tags = tags
	}
	if v.Source != nil {
		value.Source = *v.Source
	}
	if v.Visibility != nil {
		value.Visibility = *v.Visibility
	}
	if v.Sensitivity != nil {
		value.Sensitivity = *v.Sensitivity
	}

	return nil
}

type ListOptions struct {
	Kinds          []Kind        `json:"kinds,omitempty"`
	Sources        []Source      `json:"sources,omitempty"`
	Visibilities   []Visibility  `json:"visibilities,omitempty"`
	Sensitivities  []Sensitivity `json:"sensitivities,omitempty"`
	IncludeDeleted bool          `json:"include_deleted"`
	Limit          int           `json:"limit,omitempty"`
}

func (v ListOptions) Validate() error {
	for _, value := range v.Kinds {
		if !value.Valid() {
			return NewError(CodeInvalid, "invalid context kind filter")
		}
	}
	for _, value := range v.Sources {
		if !value.Valid() {
			return NewError(CodeInvalid, "invalid context source filter")
		}
	}
	for _, value := range v.Visibilities {
		if !value.Valid() {
			return NewError(CodeInvalid, "invalid context visibility filter")
		}
	}
	for _, value := range v.Sensitivities {
		if !value.Valid() {
			return NewError(CodeInvalid, "invalid context sensitivity filter")
		}
	}
	if v.Limit < 0 {
		return NewError(CodeInvalid, "limit must be non-negative")
	}

	return nil
}

type SearchOptions struct {
	Query          string `json:"query"`
	IncludeDeleted bool   `json:"include_deleted"`
	Limit          int    `json:"limit,omitempty"`
}

func (v SearchOptions) Validate() error {
	if strings.TrimSpace(v.Query) == "" {
		return NewError(CodeInvalid, "search query is required")
	}
	if v.Limit < 0 {
		return NewError(CodeInvalid, "limit must be non-negative")
	}

	return nil
}

type BuildOptions struct {
	IncludeDeleted bool `json:"include_deleted"`
	Limit          int  `json:"limit,omitempty"`
}

func (v BuildOptions) Validate() error {
	if v.Limit < 0 {
		return NewError(CodeInvalid, "limit must be non-negative")
	}

	return nil
}

type ContextPackage struct {
	SchemaVersion string               `json:"schema_version"`
	ProjectID     string               `json:"project_id"`
	GeneratedAt   time.Time            `json:"generated_at"`
	Records       []ContextPackageItem `json:"records"`
}

type ContextPackageItem struct {
	RecordID       string      `json:"record_id"`
	RecordRevision int64       `json:"record_revision"`
	ContentHash    string      `json:"content_hash"`
	Kind           Kind        `json:"kind"`
	Source         Source      `json:"source"`
	Visibility     Visibility  `json:"visibility"`
	Sensitivity    Sensitivity `json:"sensitivity"`
	Title          string      `json:"title"`
	Body           string      `json:"body"`
	Snippet        string      `json:"snippet"`
	Tags           []string    `json:"tags"`
	Comment        string      `json:"comment,omitempty"`
}

func NormalizeTags(tags []string) ([]string, error) {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			return nil, NewError(CodeInvalid, "tag must not be blank")
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	return result, nil
}

func MarshalTags(tags []string) (string, error) {
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return "", err
	}

	value, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}

	return string(value), nil
}

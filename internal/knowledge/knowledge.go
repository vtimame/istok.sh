package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
)

const (
	DefaultCatalogLimit  = 50
	MaxCatalogLimit      = 200
	SnippetBytes         = 280
	BriefingLimit        = 20
	BriefingBudgetBytes  = 4096
	MaxTitleBytes        = 200
	MaxSummaryBytes      = 500
	MaxBodyBytes         = 1024 * 1024
	MaxTags              = 20
	MaxTagBytes          = 64
	MaxProvenance        = 50
	MaxDetailBytes       = 256
	MaxProvenanceIDBytes = 512
	MaxReviewNoteBytes   = 1000
)

type Kind string

const (
	KindArchitecture Kind = "architecture"
	KindDomain       Kind = "domain"
	KindDecision     Kind = "decision"
	KindRunbook      Kind = "runbook"
	KindNote         Kind = "note"
)

func (v Kind) Valid() bool {
	return v == KindArchitecture || v == KindDomain || v == KindDecision || v == KindRunbook || v == KindNote
}

type Status string

const (
	StatusDraft      Status = "draft"
	StatusCurrent    Status = "current"
	StatusSuperseded Status = "superseded"
)

func (v Status) Valid() bool {
	return v == StatusDraft || v == StatusCurrent || v == StatusSuperseded
}

type ProvenanceType string

const (
	ProvenanceTask       ProvenanceType = "task"
	ProvenanceRun        ProvenanceType = "run"
	ProvenanceContext    ProvenanceType = "context"
	ProvenanceKnowledge  ProvenanceType = "knowledge"
	ProvenanceRepository ProvenanceType = "repository"
	ProvenanceImport     ProvenanceType = "import"
)

func (v ProvenanceType) Valid() bool {
	return v == ProvenanceTask || v == ProvenanceRun || v == ProvenanceContext || v == ProvenanceKnowledge || v == ProvenanceRepository || v == ProvenanceImport
}

type Provenance struct {
	Type     ProvenanceType `json:"type"`
	ID       string         `json:"id"`
	Revision int64          `json:"revision,omitempty"`
	Detail   string         `json:"detail,omitempty"`
}

func (v Provenance) Validate() error {
	if !v.Type.Valid() {
		return NewError(CodeInvalid, "knowledge provenance type is invalid")
	}
	if strings.TrimSpace(v.ID) == "" || v.ID != strings.TrimSpace(v.ID) {
		return NewError(CodeInvalid, "knowledge provenance id is required and must be normalized")
	}
	if len(v.ID) > MaxProvenanceIDBytes {
		return NewError(CodeInvalid, "knowledge provenance id exceeds %d bytes", MaxProvenanceIDBytes)
	}
	if v.Revision < 0 {
		return NewError(CodeInvalid, "knowledge provenance revision must be non-negative")
	}
	if len(v.Detail) > MaxDetailBytes {
		return NewError(CodeInvalid, "knowledge provenance detail exceeds %d bytes", MaxDetailBytes)
	}
	if v.Type == ProvenanceRepository {
		if filepath.IsAbs(v.ID) || filepath.Clean(v.ID) != v.ID || v.ID == "." || strings.HasPrefix(v.ID, ".."+string(filepath.Separator)) {
			return NewError(CodeInvalid, "repository provenance must be a normalized project-relative path")
		}
		return nil
	}
	if v.Type == ProvenanceImport {
		return nil
	}
	if !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "%s provenance id must be a canonical UUIDv7", v.Type)
	}

	return nil
}

type Item struct {
	ID           string                     `json:"id"`
	ProjectID    string                     `json:"project_id"`
	Revision     int64                      `json:"revision"`
	Kind         Kind                       `json:"kind"`
	Status       Status                     `json:"status"`
	Title        string                     `json:"title"`
	Summary      string                     `json:"summary"`
	Body         string                     `json:"body"`
	Tags         []string                   `json:"tags"`
	Visibility   contextmodel.Visibility    `json:"visibility"`
	Sensitivity  contextmodel.Sensitivity   `json:"sensitivity"`
	ContentHash  string                     `json:"content_hash"`
	Provenance   []Provenance               `json:"provenance"`
	ReviewedAt   *time.Time                 `json:"reviewed_at,omitempty"`
	SupersededBy *string                    `json:"superseded_by,omitempty"`
	Actor        contextmodel.ActorSnapshot `json:"actor"`
	CreatedAt    time.Time                  `json:"created_at"`
	UpdatedAt    time.Time                  `json:"updated_at"`
}

type CatalogItem struct {
	ID           string                   `json:"id"`
	ProjectID    string                   `json:"project_id"`
	Revision     int64                    `json:"revision"`
	Kind         Kind                     `json:"kind"`
	Status       Status                   `json:"status"`
	Title        string                   `json:"title"`
	Summary      string                   `json:"summary"`
	Snippet      string                   `json:"snippet"`
	Tags         []string                 `json:"tags"`
	Visibility   contextmodel.Visibility  `json:"visibility"`
	Sensitivity  contextmodel.Sensitivity `json:"sensitivity"`
	ContentHash  string                   `json:"content_hash"`
	ReviewedAt   *time.Time               `json:"reviewed_at,omitempty"`
	SupersededBy *string                  `json:"superseded_by,omitempty"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

// BriefingItem is the only knowledge representation eligible for automatic
// run injection. It intentionally excludes body, provenance, tags, and actor
// history; agents pull those fields through show/read when needed.
type BriefingItem struct {
	ID          string `json:"id"`
	Revision    int64  `json:"revision"`
	Kind        Kind   `json:"kind"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	ContentHash string `json:"content_hash"`
}

type BriefingMetadata struct {
	CandidateCount int  `json:"candidate_count"`
	SelectedCount  int  `json:"selected_count"`
	BudgetBytes    int  `json:"budget_bytes"`
	UsedBytes      int  `json:"used_bytes"`
	Truncated      bool `json:"truncated"`
}

func BuildBriefing(values []CatalogItem) ([]BriefingItem, BriefingMetadata) {
	metadata := BriefingMetadata{CandidateCount: len(values), BudgetBytes: BriefingBudgetBytes}
	result := make([]BriefingItem, 0, min(len(values), BriefingLimit))
	for _, value := range values {
		item := BriefingItem{ID: value.ID, Revision: value.Revision, Kind: value.Kind, Title: value.Title, Summary: value.Summary, ContentHash: value.ContentHash}
		encoded, _ := json.Marshal(item)
		if len(result) >= BriefingLimit || metadata.UsedBytes+len(encoded) > BriefingBudgetBytes {
			metadata.Truncated = true
			continue
		}
		metadata.UsedBytes += len(encoded)
		result = append(result, item)
	}
	metadata.SelectedCount = len(result)
	if metadata.SelectedCount < metadata.CandidateCount {
		metadata.Truncated = true
	}

	return result, metadata
}

type Event struct {
	ID           string                     `json:"id"`
	ItemID       string                     `json:"item_id"`
	Type         string                     `json:"type"`
	Body         string                     `json:"body"`
	ItemRevision int64                      `json:"item_revision"`
	Actor        contextmodel.ActorSnapshot `json:"actor"`
	CreatedAt    time.Time                  `json:"created_at"`
}

type CreateInput struct {
	ID          string                   `json:"id,omitempty"`
	ProjectID   string                   `json:"project_id"`
	Kind        Kind                     `json:"kind"`
	Title       string                   `json:"title"`
	Summary     string                   `json:"summary"`
	Body        string                   `json:"body"`
	Tags        []string                 `json:"tags"`
	Visibility  contextmodel.Visibility  `json:"visibility"`
	Sensitivity contextmodel.Sensitivity `json:"sensitivity"`
	Provenance  []Provenance             `json:"provenance"`
}

func (v CreateInput) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "knowledge item id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.ProjectID) {
		return NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if !v.Kind.Valid() {
		return NewError(CodeInvalid, "knowledge kind is invalid")
	}
	if strings.TrimSpace(v.Title) == "" {
		return NewError(CodeInvalid, "knowledge title is required")
	}
	if strings.ContainsAny(strings.TrimSpace(v.Title), "\r\n") {
		return NewError(CodeInvalid, "knowledge title must be a single line")
	}
	if len(strings.TrimSpace(v.Title)) > MaxTitleBytes {
		return NewError(CodeInvalid, "knowledge title exceeds %d bytes", MaxTitleBytes)
	}
	if strings.TrimSpace(v.Summary) == "" {
		return NewError(CodeInvalid, "knowledge summary is required")
	}
	if len(strings.TrimSpace(v.Summary)) > MaxSummaryBytes {
		return NewError(CodeInvalid, "knowledge summary exceeds %d bytes", MaxSummaryBytes)
	}
	if len(v.Body) > MaxBodyBytes {
		return NewError(CodeInvalid, "knowledge body exceeds %d bytes", MaxBodyBytes)
	}
	if !v.Visibility.Valid() {
		return NewError(CodeInvalid, "knowledge visibility is invalid")
	}
	if !v.Sensitivity.Valid() {
		return NewError(CodeInvalid, "knowledge sensitivity is invalid")
	}
	if _, err := NormalizeTags(v.Tags); err != nil {
		return err
	}
	if _, err := NormalizeProvenance(v.Provenance); err != nil {
		return err
	}

	return nil
}

type Patch struct {
	Kind        *Kind                     `json:"kind,omitempty"`
	Title       *string                   `json:"title,omitempty"`
	Summary     *string                   `json:"summary,omitempty"`
	Body        *string                   `json:"body,omitempty"`
	Tags        *[]string                 `json:"tags,omitempty"`
	Visibility  *contextmodel.Visibility  `json:"visibility,omitempty"`
	Sensitivity *contextmodel.Sensitivity `json:"sensitivity,omitempty"`
	Provenance  *[]Provenance             `json:"provenance,omitempty"`
}

func (v Patch) Validate() error {
	if v.Kind == nil && v.Title == nil && v.Summary == nil && v.Body == nil && v.Tags == nil && v.Visibility == nil && v.Sensitivity == nil && v.Provenance == nil {
		return NewError(CodeInvalid, "knowledge patch must not be empty")
	}
	if v.Kind != nil && !v.Kind.Valid() {
		return NewError(CodeInvalid, "knowledge kind is invalid")
	}
	if v.Title != nil && strings.TrimSpace(*v.Title) == "" {
		return NewError(CodeInvalid, "knowledge title must not be blank")
	}
	if v.Title != nil && strings.ContainsAny(strings.TrimSpace(*v.Title), "\r\n") {
		return NewError(CodeInvalid, "knowledge title must be a single line")
	}
	if v.Title != nil && len(strings.TrimSpace(*v.Title)) > MaxTitleBytes {
		return NewError(CodeInvalid, "knowledge title exceeds %d bytes", MaxTitleBytes)
	}
	if v.Summary != nil && strings.TrimSpace(*v.Summary) == "" {
		return NewError(CodeInvalid, "knowledge summary must not be blank")
	}
	if v.Summary != nil && len(strings.TrimSpace(*v.Summary)) > MaxSummaryBytes {
		return NewError(CodeInvalid, "knowledge summary exceeds %d bytes", MaxSummaryBytes)
	}
	if v.Body != nil && len(*v.Body) > MaxBodyBytes {
		return NewError(CodeInvalid, "knowledge body exceeds %d bytes", MaxBodyBytes)
	}
	if v.Visibility != nil && !v.Visibility.Valid() {
		return NewError(CodeInvalid, "knowledge visibility is invalid")
	}
	if v.Sensitivity != nil && !v.Sensitivity.Valid() {
		return NewError(CodeInvalid, "knowledge sensitivity is invalid")
	}
	if v.Tags != nil {
		if _, err := NormalizeTags(*v.Tags); err != nil {
			return err
		}
	}
	if v.Provenance != nil {
		if _, err := NormalizeProvenance(*v.Provenance); err != nil {
			return err
		}
	}

	return nil
}

func (v Patch) Apply(item *Item) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if item.Status != StatusDraft {
		return NewError(CodeConflict, "only draft knowledge can be updated; create a replacement draft for current knowledge")
	}
	if v.Kind != nil {
		item.Kind = *v.Kind
	}
	if v.Title != nil {
		item.Title = strings.TrimSpace(*v.Title)
	}
	if v.Summary != nil {
		item.Summary = strings.TrimSpace(*v.Summary)
	}
	if v.Body != nil {
		item.Body = *v.Body
	}
	if v.Tags != nil {
		item.Tags, _ = NormalizeTags(*v.Tags)
	}
	if v.Visibility != nil {
		item.Visibility = *v.Visibility
	}
	if v.Sensitivity != nil {
		item.Sensitivity = *v.Sensitivity
	}
	if v.Provenance != nil {
		item.Provenance, _ = NormalizeProvenance(*v.Provenance)
	}
	item.ContentHash = ContentHash(*item)
	item.ReviewedAt = nil

	return nil
}

type CatalogOptions struct {
	Kinds         []Kind                     `json:"kinds,omitempty"`
	Statuses      []Status                   `json:"statuses,omitempty"`
	Visibilities  []contextmodel.Visibility  `json:"visibilities,omitempty"`
	Sensitivities []contextmodel.Sensitivity `json:"sensitivities,omitempty"`
	Limit         int                        `json:"limit,omitempty"`
	Offset        int                        `json:"offset,omitempty"`
}

func (v CatalogOptions) Validate() error {
	for _, kind := range v.Kinds {
		if !kind.Valid() {
			return NewError(CodeInvalid, "invalid knowledge kind filter")
		}
	}
	for _, status := range v.Statuses {
		if !status.Valid() {
			return NewError(CodeInvalid, "invalid knowledge status filter")
		}
	}
	for _, visibility := range v.Visibilities {
		if !visibility.Valid() {
			return NewError(CodeInvalid, "invalid knowledge visibility filter")
		}
	}
	for _, sensitivity := range v.Sensitivities {
		if !sensitivity.Valid() {
			return NewError(CodeInvalid, "invalid knowledge sensitivity filter")
		}
	}
	if v.Limit < 0 || v.Limit > MaxCatalogLimit {
		return NewError(CodeInvalid, "knowledge limit must be between 0 and %d", MaxCatalogLimit)
	}
	if v.Offset < 0 {
		return NewError(CodeInvalid, "knowledge offset must be non-negative")
	}

	return nil
}

func (v CatalogOptions) EffectiveLimit() int {
	if v.Limit == 0 {
		return DefaultCatalogLimit
	}

	return v.Limit
}

type SearchOptions struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

func (v SearchOptions) Validate() error {
	if strings.TrimSpace(v.Query) == "" {
		return NewError(CodeInvalid, "knowledge search query is required")
	}
	return (CatalogOptions{Limit: v.Limit, Offset: v.Offset}).Validate()
}

func (v SearchOptions) EffectiveLimit() int {
	return (CatalogOptions{Limit: v.Limit}).EffectiveLimit()
}

func ContentHash(item Item) string {
	encoded, _ := json.Marshal(struct {
		Kind        Kind                     `json:"kind"`
		Title       string                   `json:"title"`
		Summary     string                   `json:"summary"`
		Body        string                   `json:"body"`
		Tags        []string                 `json:"tags"`
		Visibility  contextmodel.Visibility  `json:"visibility"`
		Sensitivity contextmodel.Sensitivity `json:"sensitivity"`
		Provenance  []Provenance             `json:"provenance"`
	}{item.Kind, item.Title, item.Summary, item.Body, item.Tags, item.Visibility, item.Sensitivity, item.Provenance})
	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:])
}

func NormalizeTags(values []string) ([]string, error) {
	if len(values) > MaxTags {
		return nil, NewError(CodeInvalid, "knowledge tags exceed the %d-item limit", MaxTags)
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, NewError(CodeInvalid, "knowledge tag must not be blank")
		}
		if len(value) > MaxTagBytes {
			return nil, NewError(CodeInvalid, "knowledge tag exceeds %d bytes", MaxTagBytes)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)

	return result, nil
}

func NormalizeProvenance(values []Provenance) ([]Provenance, error) {
	if len(values) > MaxProvenance {
		return nil, NewError(CodeInvalid, "knowledge provenance exceeds the %d-item limit", MaxProvenance)
	}
	result := append([]Provenance{}, values...)
	for i := range result {
		result[i].ID = strings.TrimSpace(result[i].ID)
		result[i].Detail = strings.TrimSpace(result[i].Detail)
		if err := result[i].Validate(); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].ID < result[j].ID
	})

	return result, nil
}

func Snippet(body string) string {
	value := strings.TrimSpace(strings.Join(strings.Fields(body), " "))
	if len(value) <= SnippetBytes {
		return value
	}

	end := SnippetBytes - 1
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}

	return strings.TrimSpace(value[:end]) + "…"
}

func ToCatalog(item Item) CatalogItem {
	return CatalogItem{
		ID: item.ID, ProjectID: item.ProjectID, Revision: item.Revision, Kind: item.Kind, Status: item.Status,
		Title: item.Title, Summary: item.Summary, Snippet: Snippet(item.Body), Tags: append([]string{}, item.Tags...),
		Visibility: item.Visibility, Sensitivity: item.Sensitivity, ContentHash: item.ContentHash,
		ReviewedAt: cloneTime(item.ReviewedAt), SupersededBy: cloneString(item.SupersededBy), UpdatedAt: item.UpdatedAt,
	}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

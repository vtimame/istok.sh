package run

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"
)

type ClaimInput struct {
	ID                      string `json:"id,omitempty"`
	SnapshotID              string `json:"snapshot_id,omitempty"`
	ContextLimit            int    `json:"context_limit"`
	BaseBranch              string `json:"base_branch"`
	BaseCommit              string `json:"base_commit"`
	WithoutRetrieval        bool   `json:"without_retrieval"`
	RetrievalOverrideReason string `json:"retrieval_override_reason"`
}

func (v ClaimInput) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "run id must be a canonical UUIDv7")
	}
	if v.SnapshotID != "" && !IsUUIDv7(v.SnapshotID) {
		return NewError(CodeInvalid, "snapshot id must be a canonical UUIDv7")
	}
	if v.ContextLimit < 0 {
		return NewError(CodeInvalid, "context limit must be non-negative")
	}
	v.RetrievalOverrideReason = strings.TrimSpace(v.RetrievalOverrideReason)
	if v.WithoutRetrieval != (v.RetrievalOverrideReason != "") {
		return NewError(CodeInvalid, "without_retrieval and retrieval_override_reason must be set together")
	}

	return nil
}

type ClaimRecord struct {
	RunID          string          `json:"run_id"`
	TaskID         string          `json:"task_id"`
	EventID        string          `json:"event_id"`
	AbandonEventID string          `json:"abandon_event_id"`
	Snapshot       ContextSnapshot `json:"snapshot"`
	BaseBranch     string          `json:"base_branch"`
	BaseCommit     string          `json:"base_commit"`
	Actor          ActorSnapshot   `json:"actor"`
	LeaseID        string          `json:"lease_id"`
	LeaseDuration  time.Duration   `json:"-"`
}

type HeartbeatInput struct {
	RunID         string        `json:"run_id"`
	LeaseID       string        `json:"lease_id"`
	LeaseDuration time.Duration `json:"-"`
}

func (v HeartbeatInput) Validate() error {
	if !IsUUIDv7(v.RunID) || !IsUUIDv7(v.LeaseID) || v.LeaseDuration <= 0 {
		return NewError(CodeInvalid, "run id, lease id, and positive lease duration are required")
	}
	return nil
}

type RecoverInput struct {
	RunID         string        `json:"run_id"`
	LeaseID       string        `json:"lease_id"`
	LeaseDuration time.Duration `json:"-"`
	Force         bool          `json:"force"`
	Reason        string        `json:"reason"`
	EventID       string        `json:"-"`
}

type AbandonInput struct {
	RunID            string `json:"run_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
	EventID          string `json:"-"`
}

func (v AbandonInput) Validate() error {
	if !IsUUIDv7(v.RunID) || !IsUUIDv7(v.EventID) || v.ExpectedRevision < 1 || strings.TrimSpace(v.Reason) == "" {
		return NewError(CodeInvalid, "run id, expected revision, event id, and reason are required")
	}
	return nil
}

func (v RecoverInput) Validate() error {
	if !IsUUIDv7(v.RunID) || !IsUUIDv7(v.LeaseID) || !IsUUIDv7(v.EventID) || v.LeaseDuration <= 0 || strings.TrimSpace(v.Reason) == "" {
		return NewError(CodeInvalid, "run id, lease id, positive lease duration, and reason are required")
	}
	return nil
}

type StartExecutionInput struct {
	ID                string   `json:"id,omitempty"`
	RunID             string   `json:"run_id"`
	LeaseID           string   `json:"lease_id"`
	Argv              []string `json:"argv"`
	CWD               string   `json:"cwd"`
	DangerousOverride string   `json:"dangerous_override,omitempty"`
}

func (v StartExecutionInput) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.RunID) {
		return NewError(CodeInvalid, "run id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.LeaseID) {
		return NewError(CodeInvalid, "lease id must be a canonical UUIDv7")
	}
	if len(v.Argv) == 0 {
		return NewError(CodeInvalid, "argv is required")
	}
	for _, arg := range v.Argv {
		if strings.TrimSpace(arg) == "" {
			return NewError(CodeInvalid, "argv entries must be non-blank")
		}
	}
	if strings.TrimSpace(v.CWD) == "" {
		return NewError(CodeInvalid, "cwd is required")
	}

	return nil
}

type FinishExecutionInput struct {
	ExecutionID      string          `json:"execution_id"`
	ExpectedRevision int64           `json:"expected_revision"`
	Status           ExecutionStatus `json:"status"`
	ExitCode         *int            `json:"exit_code,omitempty"`
	DurationMS       *int64          `json:"duration_ms,omitempty"`
	Signal           string          `json:"signal"`
	TimedOut         bool            `json:"timed_out"`
}

func (v FinishExecutionInput) Validate() error {
	if !IsUUIDv7(v.ExecutionID) {
		return NewError(CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	if v.ExpectedRevision < 1 {
		return NewError(CodeInvalid, "expected revision is required")
	}
	if !v.Status.Valid() || !v.Status.Terminal() {
		return NewError(CodeInvalid, "execution finish status must be terminal")
	}
	if v.DurationMS != nil && *v.DurationMS < 0 {
		return NewError(CodeInvalid, "duration must be non-negative")
	}

	return nil
}

type RecordValidationInput struct {
	ID          string           `json:"id,omitempty"`
	ExecutionID string           `json:"execution_id"`
	Source      ValidationSource `json:"source"`
	Status      ValidationStatus `json:"status"`
	Command     string           `json:"command"`
	ExitCode    *int             `json:"exit_code,omitempty"`
	DurationMS  *int64           `json:"duration_ms,omitempty"`
	Summary     string           `json:"summary"`
}

type ArtifactInput struct {
	ID           string       `json:"id,omitempty"`
	ExecutionID  string       `json:"execution_id"`
	Kind         ArtifactKind `json:"kind"`
	RelativePath string       `json:"relative_path"`
	SHA256       string       `json:"sha256"`
	OriginalSize int64        `json:"original_size"`
	StoredSize   int64        `json:"stored_size"`
	Truncated    bool         `json:"truncated"`
	MediaType    string       `json:"media_type"`
}

func (v ArtifactInput) Validate() error {
	if (v.ID != "" && !IsUUIDv7(v.ID)) || !IsUUIDv7(v.ExecutionID) || !v.Kind.Valid() || !validArtifactPath(v.RelativePath) || !validSHA256(v.SHA256) || v.OriginalSize < 0 || v.StoredSize < 0 || v.StoredSize > v.OriginalSize || v.Truncated != (v.StoredSize < v.OriginalSize) || strings.TrimSpace(v.MediaType) == "" {
		return NewError(CodeInvalid, "artifact fields are invalid")
	}
	return nil
}

func validArtifactPath(value string) bool {
	if strings.TrimSpace(value) == "" || strings.Contains(value, "\\") || filepath.IsAbs(value) {
		return false
	}

	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}

	_, err := hex.DecodeString(value)
	return err == nil
}

type FinishManagedExecutionInput struct {
	FinishExecutionInput
	Artifacts  []ArtifactInput        `json:"artifacts"`
	Validation *RecordValidationInput `json:"validation,omitempty"`
}

func (v FinishManagedExecutionInput) Validate() error {
	if err := v.FinishExecutionInput.Validate(); err != nil {
		return err
	}
	if len(v.Artifacts) != 2 {
		return NewError(CodeInvalid, "exactly stdout and stderr artifacts are required")
	}
	seen := map[ArtifactKind]bool{}
	for _, artifact := range v.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
		if artifact.ExecutionID != v.ExecutionID || seen[artifact.Kind] {
			return NewError(CodeInvalid, "artifacts must be unique and match execution")
		}
		seen[artifact.Kind] = true
	}
	if !seen[ArtifactKindStdout] || !seen[ArtifactKindStderr] {
		return NewError(CodeInvalid, "stdout and stderr artifacts are required")
	}
	if v.Validation != nil {
		if err := v.Validation.Validate(); err != nil {
			return err
		}
		if v.Validation.ExecutionID != v.ExecutionID {
			return NewError(CodeInvalid, "validation must match execution")
		}
	}
	return nil
}

func (v RecordValidationInput) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "validation id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.ExecutionID) {
		return NewError(CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	if !v.Source.Valid() {
		return NewError(CodeInvalid, "validation source is invalid")
	}
	if !v.Status.Valid() {
		return NewError(CodeInvalid, "validation status is invalid")
	}
	if strings.TrimSpace(v.Command) == "" {
		return NewError(CodeInvalid, "command is required")
	}
	if strings.TrimSpace(v.Summary) == "" {
		return NewError(CodeInvalid, "summary is required")
	}
	if v.DurationMS != nil && *v.DurationMS < 0 {
		return NewError(CodeInvalid, "duration must be non-negative")
	}

	return nil
}

type FinishRunInput struct {
	RunID            string `json:"run_id"`
	LeaseID          string `json:"lease_id"`
	EventID          string `json:"-"`
	ExpectedRevision int64  `json:"expected_revision"`
	Status           Status `json:"status"`
	ResultSummary    string `json:"result_summary"`
	AllowUnvalidated bool   `json:"allow_unvalidated"`
	OverrideReason   string `json:"override_reason"`
}

func (v FinishRunInput) Validate() error {
	if !IsUUIDv7(v.RunID) {
		return NewError(CodeInvalid, "run id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.LeaseID) {
		return NewError(CodeInvalid, "lease id must be a canonical UUIDv7")
	}
	if v.EventID != "" && !IsUUIDv7(v.EventID) {
		return NewError(CodeInvalid, "event id must be a canonical UUIDv7")
	}
	if v.ExpectedRevision < 1 {
		return NewError(CodeInvalid, "expected revision is required")
	}
	if !v.Status.Valid() || !v.Status.Terminal() || v.Status == StatusAbandoned {
		return NewError(CodeInvalid, "finish status must be terminal")
	}
	if strings.TrimSpace(v.ResultSummary) == "" {
		return NewError(CodeInvalid, "result summary is required")
	}
	if v.AllowUnvalidated && strings.TrimSpace(v.OverrideReason) == "" {
		return NewError(CodeInvalid, "override reason is required when unvalidated completion is allowed")
	}

	return nil
}

type CompleteTaskInput struct {
	ID                   string `json:"id,omitempty"`
	ExpectedTaskRevision int64  `json:"expected_task_revision"`
	RunID                string `json:"run_id,omitempty"`
	ValidationID         string `json:"validation_id,omitempty"`
	Note                 string `json:"note"`
	OverrideReason       string `json:"override_reason"`
}

func (v CompleteTaskInput) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "completion id must be a canonical UUIDv7")
	}
	if v.ExpectedTaskRevision < 1 {
		return NewError(CodeInvalid, "expected task revision is required")
	}
	if strings.TrimSpace(v.Note) == "" {
		return NewError(CodeInvalid, "note is required")
	}
	if v.RunID != "" && !IsUUIDv7(v.RunID) {
		return NewError(CodeInvalid, "run id must be a canonical UUIDv7")
	}
	if v.ValidationID != "" && !IsUUIDv7(v.ValidationID) {
		return NewError(CodeInvalid, "validation id must be a canonical UUIDv7")
	}
	if v.RunID == "" || v.ValidationID == "" {
		if strings.TrimSpace(v.OverrideReason) == "" {
			return NewError(CodeEvidenceRequired, "run id and validation id are required unless an override is provided")
		}
	}

	return nil
}

type CompletionRecord struct {
	ID                   string
	TaskID               string
	EventID              string
	ExpectedTaskRevision int64
	RunID                *string
	ValidationID         *string
	Note                 string
	OverrideReason       string
	Actor                ActorSnapshot
	CreatedAt            time.Time
}

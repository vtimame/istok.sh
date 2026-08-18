package run

import (
	"strings"
	"time"
)

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

type Status string

const (
	StatusActive    Status = "active"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusBlocked   Status = "blocked"
	StatusCancelled Status = "cancelled"
	StatusAbandoned Status = "abandoned"
)

func (v Status) Valid() bool {
	return v == StatusActive ||
		v == StatusSucceeded ||
		v == StatusFailed ||
		v == StatusBlocked ||
		v == StatusCancelled ||
		v == StatusAbandoned
}

func (v Status) Terminal() bool {
	return v == StatusSucceeded || v == StatusFailed || v == StatusCancelled || v == StatusBlocked || v == StatusAbandoned
}

type ExecutionStatus string

const (
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionSucceeded ExecutionStatus = "succeeded"
	ExecutionFailed    ExecutionStatus = "failed"
	ExecutionCancelled ExecutionStatus = "cancelled"
)

func (v ExecutionStatus) Valid() bool {
	return v == ExecutionRunning ||
		v == ExecutionSucceeded ||
		v == ExecutionFailed ||
		v == ExecutionCancelled
}

func (v ExecutionStatus) Terminal() bool {
	return v == ExecutionSucceeded ||
		v == ExecutionFailed ||
		v == ExecutionCancelled
}

type ValidationSource string

const (
	ValidationSourceExecuted ValidationSource = "executed"
	ValidationSourceAttested ValidationSource = "attested"
)

func (v ValidationSource) Valid() bool {
	return v == ValidationSourceExecuted || v == ValidationSourceAttested
}

type ValidationStatus string

const (
	ValidationStatusPassed ValidationStatus = "passed"
	ValidationStatusFailed ValidationStatus = "failed"
)

func (v ValidationStatus) Valid() bool {
	return v == ValidationStatusPassed || v == ValidationStatusFailed
}

type Run struct {
	ID                 string         `json:"id"`
	TaskID             string         `json:"task_id"`
	ContextSnapshotID  string         `json:"context_snapshot_id"`
	Revision           int64          `json:"revision"`
	Status             Status         `json:"status"`
	Actor              ActorSnapshot  `json:"actor"`
	BaseBranch         string         `json:"base_branch"`
	BaseCommit         string         `json:"base_commit"`
	StartedAt          time.Time      `json:"started_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	FinishedAt         *time.Time     `json:"finished_at,omitempty"`
	FinishedBy         *ActorSnapshot `json:"finished_by,omitempty"`
	ResultSummary      string         `json:"result_summary"`
	ValidationOverride string         `json:"validation_override,omitempty"`
	LeaseID            string         `json:"lease_id"`
	LeaseOwner         ActorSnapshot  `json:"lease_owner"`
	HeartbeatAt        time.Time      `json:"heartbeat_at"`
	ExpiresAt          time.Time      `json:"expires_at"`
}

type ArtifactKind string

const (
	ArtifactKindStdout ArtifactKind = "stdout"
	ArtifactKindStderr ArtifactKind = "stderr"
)

func (v ArtifactKind) Valid() bool { return v == ArtifactKindStdout || v == ArtifactKindStderr }

type Artifact struct {
	ID           string        `json:"id"`
	RunID        string        `json:"run_id"`
	ExecutionID  string        `json:"execution_id"`
	Kind         ArtifactKind  `json:"kind"`
	RelativePath string        `json:"relative_path"`
	SHA256       string        `json:"sha256"`
	OriginalSize int64         `json:"original_size"`
	StoredSize   int64         `json:"stored_size"`
	Truncated    bool          `json:"truncated"`
	MediaType    string        `json:"media_type"`
	Actor        ActorSnapshot `json:"actor"`
	CreatedAt    time.Time     `json:"created_at"`
}

type FinishManagedExecutionResult struct {
	Execution  Execution   `json:"execution"`
	Artifacts  []Artifact  `json:"artifacts"`
	Validation *Validation `json:"validation,omitempty"`
}

type Execution struct {
	ID                string          `json:"id"`
	RunID             string          `json:"run_id"`
	Revision          int64           `json:"revision"`
	Status            ExecutionStatus `json:"status"`
	Argv              []string        `json:"argv"`
	CWD               string          `json:"cwd"`
	ExitCode          *int            `json:"exit_code,omitempty"`
	DurationMS        *int64          `json:"duration_ms,omitempty"`
	Signal            string          `json:"signal"`
	TimedOut          bool            `json:"timed_out"`
	DangerousOverride string          `json:"dangerous_override,omitempty"`
	Actor             ActorSnapshot   `json:"actor"`
	StartedAt         time.Time       `json:"started_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	FinishedAt        *time.Time      `json:"finished_at,omitempty"`
}

type Validation struct {
	ID          string           `json:"id"`
	ExecutionID string           `json:"execution_id"`
	Source      ValidationSource `json:"source"`
	Status      ValidationStatus `json:"status"`
	Command     string           `json:"command"`
	ExitCode    *int             `json:"exit_code,omitempty"`
	DurationMS  *int64           `json:"duration_ms,omitempty"`
	Summary     string           `json:"summary"`
	Actor       ActorSnapshot    `json:"actor"`
	CreatedAt   time.Time        `json:"created_at"`
}

type Completion struct {
	ID             string        `json:"id"`
	TaskID         string        `json:"task_id"`
	RunID          *string       `json:"run_id,omitempty"`
	ValidationID   *string       `json:"validation_id,omitempty"`
	Note           string        `json:"note"`
	OverrideReason string        `json:"override_reason"`
	Actor          ActorSnapshot `json:"actor"`
	CreatedAt      time.Time     `json:"created_at"`
}

type Show struct {
	Run         Run             `json:"run"`
	Snapshot    ContextSnapshot `json:"snapshot"`
	Executions  []Execution     `json:"executions"`
	Validations []Validation    `json:"validations"`
	Artifacts   []Artifact      `json:"artifacts"`
}

type TaskRunState struct {
	HasActiveRun  bool
	HasExpiredRun bool
}

type ListOptions struct {
	ProjectID string   `json:"project_id,omitempty"`
	TaskID    string   `json:"task_id,omitempty"`
	Statuses  []Status `json:"statuses,omitempty"`
	Limit     int      `json:"limit"`
}

func (v ListOptions) Validate() error {
	if v.ProjectID != "" && !IsUUIDv7(v.ProjectID) {
		return NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if v.TaskID != "" && !IsUUIDv7(v.TaskID) {
		return NewError(CodeInvalid, "task id must be a canonical UUIDv7")
	}
	for _, status := range v.Statuses {
		if !status.Valid() {
			return NewError(CodeInvalid, "invalid run status filter")
		}
	}
	if v.Limit < 0 {
		return NewError(CodeInvalid, "limit must be non-negative")
	}

	return nil
}

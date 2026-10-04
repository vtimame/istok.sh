package cli

import (
	"time"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
	runmodel "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type actorJSON struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type projectRootJSON struct {
	CanonicalPath string     `json:"canonical_path"`
	ActiveAt      time.Time  `json:"active_at"`
	DetachedAt    *time.Time `json:"detached_at,omitempty"`
}

type projectJSON struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Revision  int64            `json:"revision"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	DeletedAt *time.Time       `json:"deleted_at,omitempty"`
	Root      *projectRootJSON `json:"root,omitempty"`
}

type taskValueJSON struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"project_id"`
	Number             int64      `json:"number"`
	Revision           int64      `json:"revision"`
	Status             string     `json:"status"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	AcceptanceCriteria string     `json:"acceptance_criteria"`
	Notes              string     `json:"notes"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
}

type taskEventJSON struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	Type         string    `json:"type"`
	Body         string    `json:"body"`
	TaskRevision int64     `json:"task_revision"`
	Actor        actorJSON `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
}

type taskDependencyJSON struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	BlockerTaskID string    `json:"blocker_task_id"`
	BlockedTaskID string    `json:"blocked_task_id"`
	EdgeType      string    `json:"edge_type"`
	CreatedAt     time.Time `json:"created_at"`
}

type taskSummaryJSON struct {
	ID        string     `json:"id"`
	Number    int64      `json:"number"`
	Status    string     `json:"status"`
	Title     string     `json:"title"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type runJSON struct {
	ID                 string     `json:"id"`
	TaskID             string     `json:"task_id"`
	ContextSnapshotID  string     `json:"context_snapshot_id"`
	Revision           int64      `json:"revision"`
	Status             string     `json:"status"`
	Actor              actorJSON  `json:"actor"`
	BaseBranch         string     `json:"base_branch"`
	BaseCommit         string     `json:"base_commit"`
	StartedAt          time.Time  `json:"started_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	FinishedBy         *actorJSON `json:"finished_by,omitempty"`
	ResultSummary      string     `json:"result_summary"`
	ValidationOverride string     `json:"validation_override,omitempty"`
	LeaseID            string     `json:"lease_id"`
	LeaseOwner         actorJSON  `json:"lease_owner"`
	HeartbeatAt        time.Time  `json:"heartbeat_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
}

type executionJSON struct {
	ID                string     `json:"id"`
	RunID             string     `json:"run_id"`
	Revision          int64      `json:"revision"`
	Status            string     `json:"status"`
	Argv              []string   `json:"argv"`
	CWD               string     `json:"cwd"`
	ExitCode          *int       `json:"exit_code,omitempty"`
	DurationMS        *int64     `json:"duration_ms,omitempty"`
	Signal            string     `json:"signal"`
	TimedOut          bool       `json:"timed_out"`
	DangerousOverride string     `json:"dangerous_override,omitempty"`
	Actor             actorJSON  `json:"actor"`
	StartedAt         time.Time  `json:"started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
}

type artifactJSON struct {
	ID           string    `json:"id"`
	RunID        string    `json:"run_id"`
	ExecutionID  string    `json:"execution_id"`
	Kind         string    `json:"kind"`
	RelativePath string    `json:"relative_path"`
	SHA256       string    `json:"sha256"`
	OriginalSize int64     `json:"original_size"`
	StoredSize   int64     `json:"stored_size"`
	Truncated    bool      `json:"truncated"`
	MediaType    string    `json:"media_type"`
	Actor        actorJSON `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
}

type validationJSON struct {
	ID          string    `json:"id"`
	ExecutionID string    `json:"execution_id"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
	Command     string    `json:"command"`
	ExitCode    *int      `json:"exit_code,omitempty"`
	DurationMS  *int64    `json:"duration_ms,omitempty"`
	Summary     string    `json:"summary"`
	Actor       actorJSON `json:"actor"`
	CreatedAt   time.Time `json:"created_at"`
}

type snapshotItemJSON struct {
	RecordID       string                 `json:"record_id"`
	RecordRevision int64                  `json:"record_revision"`
	ContentHash    string                 `json:"content_hash"`
	Kind           string                 `json:"kind"`
	Source         string                 `json:"source"`
	Visibility     string                 `json:"visibility"`
	Sensitivity    string                 `json:"sensitivity"`
	Delivery       string                 `json:"delivery,omitempty"`
	Enabled        *bool                  `json:"enabled,omitempty"`
	Priority       *contextmodel.Priority `json:"priority,omitempty"`
	Scope          *contextmodel.Scope    `json:"scope,omitempty"`
	Title          string                 `json:"title"`
	Body           string                 `json:"body"`
	Snippet        string                 `json:"snippet"`
	Tags           []string               `json:"tags"`
	Lane           string                 `json:"lane,omitempty"`
	Score          float64                `json:"score,omitempty"`
	MatchedTerms   []string               `json:"matched_terms,omitempty"`
	Reasons        []string               `json:"reasons,omitempty"`
}

type snapshotJSON struct {
	ID            string                                    `json:"id"`
	SchemaVersion string                                    `json:"schema_version"`
	ProjectID     string                                    `json:"project_id"`
	GeneratedAt   time.Time                                 `json:"generated_at"`
	CreatedAt     time.Time                                 `json:"created_at"`
	Records       []snapshotItemJSON                        `json:"records"`
	Retrieval     []runmodel.ContextSnapshotRetrievalItem   `json:"retrieval"`
	Knowledge     []knowledge.BriefingItem                  `json:"knowledge_catalog"`
	Metadata      runmodel.ContextSnapshotRetrievalMetadata `json:"metadata"`
}

type runShowJSON struct {
	Run         runJSON          `json:"run"`
	Snapshot    snapshotJSON     `json:"snapshot"`
	Executions  []executionJSON  `json:"executions"`
	Validations []validationJSON `json:"validations"`
	Artifacts   []artifactJSON   `json:"artifacts"`
	TaskNumber  int64            `json:"task_number,omitempty"`
}

type managedExecutionJSON struct {
	Execution  executionJSON   `json:"execution"`
	Artifacts  []artifactJSON  `json:"artifacts"`
	Validation *validationJSON `json:"validation,omitempty"`
}

type runListOutputJSON struct {
	runJSON
	TaskNumber int64 `json:"task_number"`
}

type artifactVerificationJSON struct {
	Artifact artifactJSON `json:"artifact"`
	Verified bool         `json:"verified"`
}

type taskCompletionJSON struct {
	Task       taskValueJSON  `json:"task"`
	Completion completionJSON `json:"completion"`
}

type completionJSON struct {
	ID             string    `json:"id"`
	TaskID         string    `json:"task_id"`
	RunID          *string   `json:"run_id,omitempty"`
	ValidationID   *string   `json:"validation_id,omitempty"`
	Note           string    `json:"note"`
	OverrideReason string    `json:"override_reason"`
	Actor          actorJSON `json:"actor"`
	CreatedAt      time.Time `json:"created_at"`
}

func actorJSONValue(value runmodel.ActorSnapshot) actorJSON {
	return actorJSON{ID: value.ID, Kind: value.Kind, Name: value.Name}
}

func taskActorJSONValue(value task.ActorSnapshot) actorJSON {
	return actorJSON{ID: value.ID, Kind: value.Kind, Name: value.Name}
}

func projectJSONValue(value project.Project) projectJSON {
	result := projectJSON{ID: value.ID, Name: value.Name, Revision: value.Revision, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, DeletedAt: value.DeletedAt}
	if value.Root != nil {
		result.Root = &projectRootJSON{CanonicalPath: value.Root.CanonicalPath, ActiveAt: value.Root.ActiveAt, DetachedAt: value.Root.DetachedAt}
	}

	return result
}

func taskJSONValue(value task.Task) taskValueJSON {
	return taskValueJSON{
		ID: value.ID, ProjectID: value.ProjectID, Number: value.Number, Revision: value.Revision,
		Status: string(value.Status), Title: value.Title, Description: value.Description,
		AcceptanceCriteria: value.AcceptanceCriteria, Notes: value.Notes,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, DeletedAt: value.DeletedAt,
	}
}

func runJSONValue(value runmodel.Run) runJSON {
	result := runJSON{
		ID: value.ID, TaskID: value.TaskID, ContextSnapshotID: value.ContextSnapshotID,
		Revision: value.Revision, Status: string(value.Status), Actor: actorJSONValue(value.Actor),
		BaseBranch: value.BaseBranch, BaseCommit: value.BaseCommit, StartedAt: value.StartedAt,
		UpdatedAt: value.UpdatedAt, FinishedAt: value.FinishedAt, ResultSummary: value.ResultSummary,
		ValidationOverride: value.ValidationOverride, LeaseID: value.LeaseID,
		LeaseOwner: actorJSONValue(value.LeaseOwner), HeartbeatAt: value.HeartbeatAt, ExpiresAt: value.ExpiresAt,
	}
	if value.FinishedBy != nil {
		actor := actorJSONValue(*value.FinishedBy)
		result.FinishedBy = &actor
	}

	return result
}

func executionJSONValue(value runmodel.Execution) executionJSON {
	return executionJSON{
		ID: value.ID, RunID: value.RunID, Revision: value.Revision, Status: string(value.Status),
		Argv: nonNilStrings(value.Argv), CWD: value.CWD, ExitCode: value.ExitCode, DurationMS: value.DurationMS,
		Signal: value.Signal, TimedOut: value.TimedOut, DangerousOverride: value.DangerousOverride,
		Actor: actorJSONValue(value.Actor), StartedAt: value.StartedAt, UpdatedAt: value.UpdatedAt, FinishedAt: value.FinishedAt,
	}
}

func artifactJSONValue(value runmodel.Artifact) artifactJSON {
	return artifactJSON{
		ID: value.ID, RunID: value.RunID, ExecutionID: value.ExecutionID, Kind: string(value.Kind),
		RelativePath: value.RelativePath, SHA256: value.SHA256, OriginalSize: value.OriginalSize,
		StoredSize: value.StoredSize, Truncated: value.Truncated, MediaType: value.MediaType,
		Actor: actorJSONValue(value.Actor), CreatedAt: value.CreatedAt,
	}
}

func validationJSONValue(value runmodel.Validation) validationJSON {
	return validationJSON{
		ID: value.ID, ExecutionID: value.ExecutionID, Source: string(value.Source), Status: string(value.Status),
		Command: value.Command, ExitCode: value.ExitCode, DurationMS: value.DurationMS, Summary: value.Summary,
		Actor: actorJSONValue(value.Actor), CreatedAt: value.CreatedAt,
	}
}

func snapshotJSONValue(value runmodel.ContextSnapshot) snapshotJSON {
	records := make([]snapshotItemJSON, 0, len(value.Records))
	for _, record := range value.Records {
		records = append(records, snapshotItemJSON{
			RecordID: record.RecordID, RecordRevision: record.RecordRevision, ContentHash: record.ContentHash,
			Kind: string(record.Kind), Source: string(record.Source), Visibility: string(record.Visibility),
			Sensitivity: string(record.Sensitivity), Delivery: string(record.Delivery), Enabled: record.Enabled,
			Priority: record.Priority, Scope: record.Scope, Title: record.Title, Body: record.Body,
			Snippet: record.Snippet, Tags: nonNilStrings(record.Tags), Lane: record.Lane, Score: record.Score,
			MatchedTerms: nonNilStrings(record.MatchedTerms), Reasons: nonNilStrings(record.Reasons),
		})
	}

	return snapshotJSON{ID: value.ID, SchemaVersion: value.SchemaVersion, ProjectID: value.ProjectID, GeneratedAt: value.GeneratedAt, CreatedAt: value.CreatedAt, Records: records, Retrieval: append([]runmodel.ContextSnapshotRetrievalItem{}, value.Retrieval...), Knowledge: append([]knowledge.BriefingItem{}, value.Knowledge...), Metadata: value.Metadata}
}

func runShowJSONValue(value runmodel.Show, taskNumber int64) runShowJSON {
	executions := make([]executionJSON, 0, len(value.Executions))
	for _, execution := range value.Executions {
		executions = append(executions, executionJSONValue(execution))
	}
	validations := make([]validationJSON, 0, len(value.Validations))
	for _, validation := range value.Validations {
		validations = append(validations, validationJSONValue(validation))
	}

	return runShowJSON{
		Run: runJSONValue(value.Run), Snapshot: snapshotJSONValue(value.Snapshot),
		Executions: executions, Validations: validations, Artifacts: artifactsJSON(value.Artifacts),
		TaskNumber: taskNumber,
	}
}

func artifactsJSON(values []runmodel.Artifact) []artifactJSON {
	result := make([]artifactJSON, 0, len(values))
	for _, value := range values {
		result = append(result, artifactJSONValue(value))
	}

	return result
}

func runOutputJSON(value any) any {
	switch typed := value.(type) {
	case runmodel.Run:
		return runJSONValue(typed)
	case []runListItem:
		result := make([]runListOutputJSON, 0, len(typed))
		for _, item := range typed {
			result = append(result, runListOutputJSON{runJSON: runJSONValue(item.Run), TaskNumber: item.TaskNumber})
		}
		return result
	case runmodel.Show:
		return runShowJSONValue(typed, 0)
	case runShowView:
		return runShowJSONValue(typed.Show, typed.TaskNumber)
	case runmodel.Execution:
		return executionJSONValue(typed)
	case runmodel.Validation:
		return validationJSONValue(typed)
	case runmodel.FinishManagedExecutionResult:
		result := managedExecutionJSON{Execution: executionJSONValue(typed.Execution), Artifacts: artifactsJSON(typed.Artifacts)}
		if typed.Validation != nil {
			validation := validationJSONValue(*typed.Validation)
			result.Validation = &validation
		}
		return result
	case []runmodel.Artifact:
		return artifactsJSON(typed)
	case artifactVerificationResult:
		return artifactVerificationJSON{Artifact: artifactJSONValue(typed.Artifact), Verified: typed.Verified}
	case taskCompletionResult:
		return taskCompletionJSON{Task: taskJSONValue(typed.Task), Completion: completionJSONValue(typed.Completion)}
	default:
		return value
	}
}

func completionJSONValue(value runmodel.Completion) completionJSON {
	return completionJSON{
		ID: value.ID, TaskID: value.TaskID, RunID: value.RunID, ValidationID: value.ValidationID,
		Note: value.Note, OverrideReason: value.OverrideReason, Actor: actorJSONValue(value.Actor), CreatedAt: value.CreatedAt,
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}

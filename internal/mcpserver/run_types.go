package mcpserver

import (
	"path/filepath"
	"time"

	"s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

const runSchemaVersion = "2"

type RunResult struct {
	SchemaVersion string               `json:"schema_version"`
	Run           *run.Run             `json:"run,omitempty"`
	Snapshot      *run.ContextSnapshot `json:"snapshot,omitempty"`
	Error         *ToolError           `json:"error,omitempty"`
}

type RunListResult struct {
	SchemaVersion string     `json:"schema_version"`
	Runs          []run.Run  `json:"runs"`
	Error         *ToolError `json:"error,omitempty"`
}

type RunShowResult struct {
	SchemaVersion string     `json:"schema_version"`
	Show          *run.Show  `json:"show,omitempty"`
	Error         *ToolError `json:"error,omitempty"`
}

type ExecutionResult struct {
	SchemaVersion string         `json:"schema_version"`
	Execution     *run.Execution `json:"execution,omitempty"`
	Error         *ToolError     `json:"error,omitempty"`
}

type ValidationResult struct {
	SchemaVersion string          `json:"schema_version"`
	Validation    *run.Validation `json:"validation,omitempty"`
	Error         *ToolError      `json:"error,omitempty"`
}

type CompletionResult struct {
	SchemaVersion string          `json:"schema_version"`
	Task          *task.Task      `json:"task,omitempty"`
	Completion    *run.Completion `json:"completion,omitempty"`
	Error         *ToolError      `json:"error,omitempty"`
}

type ManagedExecutionResult struct {
	SchemaVersion string          `json:"schema_version"`
	Execution     *run.Execution  `json:"execution,omitempty"`
	Artifacts     []run.Artifact  `json:"artifacts"`
	Validation    *run.Validation `json:"validation,omitempty"`
	Error         *ToolError      `json:"error,omitempty"`
}

type ArtifactListResult struct {
	SchemaVersion string         `json:"schema_version"`
	Artifacts     []run.Artifact `json:"artifacts"`
	Error         *ToolError     `json:"error,omitempty"`
}

type ArtifactVerifyResult struct {
	SchemaVersion string        `json:"schema_version"`
	Artifact      *run.Artifact `json:"artifact,omitempty"`
	Verified      bool          `json:"verified"`
	Error         *ToolError    `json:"error,omitempty"`
}

type runClaimInput struct {
	TaskID                  string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	RunID                   string `json:"run_id,omitempty" jsonschema:"Optional canonical UUIDv7 run identifier."`
	SnapshotID              string `json:"snapshot_id,omitempty" jsonschema:"Optional canonical UUIDv7 snapshot identifier."`
	ContextLimit            int    `json:"context_limit,omitempty" jsonschema:"Maximum number of context records to include in run snapshots."`
	BaseBranch              string `json:"base_branch,omitempty" jsonschema:"Optional task base branch for reporting and traceability."`
	BaseCommit              string `json:"base_commit,omitempty" jsonschema:"Optional task base commit for reporting and traceability."`
	WithoutRetrieval        bool   `json:"without_retrieval,omitempty" jsonschema:"Skip local task retrieval; requires retrieval_override_reason."`
	RetrievalOverrideReason string `json:"retrieval_override_reason,omitempty" jsonschema:"Audited reason required with without_retrieval."`
}

type runListInput struct {
	TaskID   string       `json:"task_id,omitempty" jsonschema:"Optional canonical UUIDv7 task identifier to filter runs."`
	Statuses []run.Status `json:"statuses,omitempty" jsonschema:"Optional run statuses to include: active, succeeded, failed, blocked, cancelled, abandoned."`
	Limit    int          `json:"limit,omitempty" jsonschema:"Maximum runs to return."`
}

type runShowInput struct {
	RunID string `json:"run_id" jsonschema:"Canonical UUIDv7 run identifier."`
}

type managedExecutionInput struct {
	RunID              string   `json:"run_id" jsonschema:"Canonical UUIDv7 run identifier."`
	LeaseID            string   `json:"lease_id" jsonschema:"Current run lease UUIDv7."`
	Argv               []string `json:"argv" jsonschema:"Managed command argv without a shell."`
	CWD                string   `json:"cwd,omitempty" jsonschema:"Optional relative working directory inside the fixed project directory."`
	TimeoutMS          *int64   `json:"timeout_ms,omitempty" jsonschema:"Positive command timeout in milliseconds."`
	TerminationGraceMS *int64   `json:"termination_grace_ms,omitempty" jsonschema:"Positive termination grace in milliseconds."`
	OutputLimit        *int64   `json:"output_limit,omitempty" jsonschema:"Positive per-stream output limit in bytes."`
	AllowDangerous     bool     `json:"allow_dangerous,omitempty" jsonschema:"Allow a dangerous command with an audited reason."`
	OverrideReason     string   `json:"override_reason,omitempty" jsonschema:"Required with allow_dangerous for dangerous commands."`
}

const maxDurationMilliseconds = int64((1<<63 - 1) / int64(time.Millisecond))

func (v managedExecutionInput) validate() error {
	if !run.IsUUIDv7(v.RunID) || !run.IsUUIDv7(v.LeaseID) || len(v.Argv) == 0 {
		return run.NewError(run.CodeInvalid, "run_id, lease_id, and argv are required")
	}
	if !validDurationMilliseconds(v.TimeoutMS) || !validDurationMilliseconds(v.TerminationGraceMS) {
		return run.NewError(run.CodeInvalid, "managed execution durations must be positive and representable")
	}
	if v.OutputLimit != nil && *v.OutputLimit <= 0 {
		return run.NewError(run.CodeInvalid, "output_limit must be positive")
	}
	if v.CWD != "" && filepath.IsAbs(v.CWD) {
		return run.NewError(run.CodeInvalid, "cwd must be relative to the project root")
	}

	return nil
}

func validDurationMilliseconds(value *int64) bool {
	return value == nil || (*value > 0 && *value <= maxDurationMilliseconds)
}

type runHeartbeatInput struct {
	RunID   string `json:"run_id"`
	LeaseID string `json:"lease_id"`
}

type runArtifactListInput struct {
	RunID string `json:"run_id"`
}

type runArtifactVerifyInput struct {
	RunID      string `json:"run_id"`
	ArtifactID string `json:"artifact_id"`
}

type runRecoverInput struct {
	RunID  string `json:"run_id"`
	Force  bool   `json:"force,omitempty"`
	Reason string `json:"reason"`
}

type runAbandonInput struct {
	RunID            string `json:"run_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}

type executionStartInput struct {
	RunID       string   `json:"run_id" jsonschema:"Canonical UUIDv7 run identifier."`
	LeaseID     string   `json:"lease_id" jsonschema:"Current run lease UUIDv7."`
	ExecutionID string   `json:"execution_id,omitempty" jsonschema:"Optional canonical UUIDv7 execution identifier."`
	Argv        []string `json:"argv" jsonschema:"Execution command arguments."`
	CWD         string   `json:"cwd" jsonschema:"Working directory for the execution."`
}

type executionFinishInput struct {
	RunID            string              `json:"run_id" jsonschema:"Canonical UUIDv7 run identifier."`
	ExecutionID      string              `json:"execution_id" jsonschema:"Canonical UUIDv7 execution identifier."`
	ExpectedRevision int64               `json:"expected_revision" jsonschema:"Positive current execution revision required for compare-and-swap."`
	Status           run.ExecutionStatus `json:"status" jsonschema:"Execution terminal status: succeeded, failed, or cancelled."`
	ExitCode         *int                `json:"exit_code,omitempty" jsonschema:"Optional process exit code."`
	DurationMS       *int64              `json:"duration_ms,omitempty" jsonschema:"Optional execution duration in milliseconds."`
	Signal           string              `json:"signal" jsonschema:"Process signal if applicable."`
	TimedOut         bool                `json:"timed_out" jsonschema:"Whether execution ended due to timeout."`
}

type validationRecordInput struct {
	RunID        string               `json:"run_id" jsonschema:"Canonical UUIDv7 run identifier."`
	ValidationID string               `json:"validation_id,omitempty" jsonschema:"Optional canonical UUIDv7 validation identifier."`
	ExecutionID  string               `json:"execution_id" jsonschema:"Canonical UUIDv7 execution identifier."`
	Source       run.ValidationSource `json:"source" jsonschema:"Manual validation source; must be attested."`
	Status       run.ValidationStatus `json:"status" jsonschema:"Validation status: passed or failed."`
	Command      string               `json:"command" jsonschema:"Executed command line."`
	ExitCode     *int                 `json:"exit_code,omitempty" jsonschema:"Optional exit code."`
	DurationMS   *int64               `json:"duration_ms,omitempty" jsonschema:"Optional duration in milliseconds."`
	Summary      string               `json:"summary" jsonschema:"Short validation summary."`
}

type runFinishInput struct {
	RunID            string     `json:"run_id" jsonschema:"Canonical UUIDv7 run identifier."`
	LeaseID          string     `json:"lease_id" jsonschema:"Current run lease UUIDv7."`
	ExpectedRevision int64      `json:"expected_revision" jsonschema:"Positive current run revision required for compare-and-swap."`
	Status           run.Status `json:"status" jsonschema:"Run terminal status: succeeded, failed, blocked, or cancelled."`
	ResultSummary    string     `json:"result_summary" jsonschema:"Required final run summary."`
	AllowUnvalidated *bool      `json:"allow_unvalidated,omitempty" jsonschema:"Allow successful run completion without passed validation (requires override_reason)."`
	OverrideReason   string     `json:"override_reason,omitempty" jsonschema:"Required when allowing unvalidated successful completion."`
}

type taskCompleteInput struct {
	TaskID               string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	ExpectedTaskRevision int64  `json:"expected_task_revision" jsonschema:"Positive current task revision required for compare-and-swap."`
	CompletionID         string `json:"completion_id,omitempty" jsonschema:"Optional canonical UUIDv7 completion identifier."`
	RunID                string `json:"run_id,omitempty" jsonschema:"Optional canonical UUIDv7 run identifier to tie completion evidence."`
	ValidationID         string `json:"validation_id,omitempty" jsonschema:"Optional canonical UUIDv7 validation identifier to tie completion evidence."`
	Note                 string `json:"note" jsonschema:"Required task completion note."`
	OverrideReason       string `json:"override_reason,omitempty" jsonschema:"Override reason when evidence is missing."`
}

package cli

import (
	"time"

	runmodel "s26.dev/istok-cli/internal/run"
)

type RunCommand struct {
	List       RunListCommand       `cmd:"" help:"List runs in the current project."`
	Show       RunShowCommand       `cmd:"" help:"Show a run with its immutable context and evidence."`
	Exec       RunExecCommand       `cmd:"" help:"Execute a command and store its output as managed evidence."`
	Validate   RunValidateCommand   `cmd:"" help:"Execute a validation command and derive evidence from its result."`
	Heartbeat  RunHeartbeatCommand  `cmd:"" help:"Extend an active run lease."`
	Recover    RunRecoverCommand    `cmd:"" help:"Recover an expired or explicitly overridden run lease."`
	Artifact   RunArtifactCommand   `cmd:"" help:"List and verify managed run artifacts."`
	Finish     RunFinishCommand     `cmd:"" help:"Finish an active run."`
	Execution  RunExecutionCommand  `cmd:"" help:"Record process executions."`
	Validation RunValidationCommand `cmd:"" help:"Record execution validations."`
}

type RunListCommand struct {
	Task     int64             `name:"task" help:"Only runs for this project-scoped task number."`
	Status   []runmodel.Status `name:"status" enum:"active,succeeded,failed,blocked,cancelled" help:"Run status filter; can be repeated."`
	Limit    int               `name:"limit" help:"Maximum number of runs to return."`
	Database string            `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool              `name:"json" help:"Write a versioned JSON response."`
}

type RunShowCommand struct {
	ID       string `arg:"" help:"Canonical UUIDv7 run ID."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type RunFinishCommand struct {
	ID               string          `arg:"" help:"Canonical UUIDv7 run ID."`
	LeaseID          string          `name:"lease" required:"" help:"Current run lease UUIDv7."`
	ExpectedRevision int64           `name:"expected-revision" required:"" help:"Current run revision required for compare-and-swap."`
	Status           runmodel.Status `name:"status" enum:"succeeded,failed,blocked,cancelled" required:"" help:"Terminal run status."`
	Summary          string          `name:"summary" required:"" help:"Run result summary."`
	AllowUnvalidated bool            `name:"allow-unvalidated" help:"Allow success without a passed validation."`
	OverrideReason   string          `name:"override-reason" help:"Audited reason required with --allow-unvalidated."`
	Database         string          `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool            `name:"json" help:"Write a versioned JSON response."`
}

type RunExecCommand struct {
	ID               string        `arg:"" help:"Canonical UUIDv7 active run ID."`
	Argv             []string      `arg:"" required:"" help:"Command argv; use -- before command flags."`
	LeaseID          string        `name:"lease" required:"" help:"Current run lease UUIDv7 returned by claim or recovery."`
	CWD              string        `name:"cwd" default:"." help:"Working directory inside the current project."`
	Timeout          time.Duration `name:"timeout" default:"30m" help:"Maximum command duration."`
	TerminationGrace time.Duration `name:"termination-grace" default:"5s" help:"Grace period between TERM and KILL."`
	OutputLimit      int64         `name:"output-limit" default:"1048576" help:"Maximum stored bytes for each output stream."`
	AllowDangerous   bool          `name:"allow-dangerous" help:"Allow a command classified as dangerous."`
	OverrideReason   string        `name:"override-reason" help:"Audited reason required with --allow-dangerous."`
	Database         string        `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool          `name:"json" help:"Write a versioned JSON response."`
}

type RunValidateCommand struct {
	ID               string        `arg:"" help:"Canonical UUIDv7 active run ID."`
	Argv             []string      `arg:"" required:"" help:"Validation argv; use -- before command flags."`
	LeaseID          string        `name:"lease" required:"" help:"Current run lease UUIDv7 returned by claim or recovery."`
	Summary          string        `name:"summary" help:"Validation summary; derived from the result when omitted."`
	CWD              string        `name:"cwd" default:"." help:"Working directory inside the current project."`
	Timeout          time.Duration `name:"timeout" default:"10m" help:"Maximum validation duration."`
	TerminationGrace time.Duration `name:"termination-grace" default:"5s" help:"Grace period between TERM and KILL."`
	OutputLimit      int64         `name:"output-limit" default:"1048576" help:"Maximum stored bytes for each output stream."`
	AllowDangerous   bool          `name:"allow-dangerous" help:"Allow a command classified as dangerous."`
	OverrideReason   string        `name:"override-reason" help:"Audited reason required with --allow-dangerous."`
	Database         string        `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool          `name:"json" help:"Write a versioned JSON response."`
}

type RunHeartbeatCommand struct {
	ID       string `arg:"" help:"Canonical UUIDv7 active run ID."`
	LeaseID  string `name:"lease" required:"" help:"Current run lease UUIDv7."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type RunRecoverCommand struct {
	ID       string `arg:"" help:"Canonical UUIDv7 active run ID."`
	Reason   string `name:"reason" required:"" help:"Audited recovery reason."`
	Force    bool   `name:"force" help:"Take over a lease before it expires."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type RunArtifactCommand struct {
	List   RunArtifactListCommand   `cmd:"" help:"List managed artifacts for a run."`
	Verify RunArtifactVerifyCommand `cmd:"" help:"Verify a managed artifact size and SHA-256."`
}

type RunArtifactListCommand struct {
	RunID    string `arg:"" help:"Canonical UUIDv7 run ID."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type RunArtifactVerifyCommand struct {
	ArtifactID string `arg:"" help:"Canonical UUIDv7 artifact ID."`
	RunID      string `name:"run" required:"" help:"Run ID used to enforce current-project scope."`
	Database   string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON       bool   `name:"json" help:"Write a versioned JSON response."`
}

type RunExecutionCommand struct {
	Start  RunExecutionStartCommand  `cmd:"" help:"Record a started process execution."`
	Finish RunExecutionFinishCommand `cmd:"" help:"Record a process execution outcome."`
}

type RunExecutionStartCommand struct {
	RunID       string   `arg:"" help:"Canonical UUIDv7 active run ID."`
	Argv        []string `arg:"" required:"" help:"Process argv."`
	LeaseID     string   `name:"lease" required:"" help:"Current run lease UUIDv7."`
	ExecutionID string   `name:"execution-id" help:"Client-generated canonical UUIDv7 execution ID."`
	CWD         string   `name:"cwd" default:"." help:"Process working directory."`
	Database    string   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON        bool     `name:"json" help:"Write a versioned JSON response."`
}

type RunExecutionFinishCommand struct {
	RunID            string                   `name:"run" required:"" help:"Run ID used to enforce current-project scope."`
	ExecutionID      string                   `arg:"" help:"Canonical UUIDv7 execution ID."`
	ExpectedRevision int64                    `name:"expected-revision" required:"" help:"Current execution revision required for compare-and-swap."`
	Status           runmodel.ExecutionStatus `name:"status" enum:"succeeded,failed,cancelled" required:"" help:"Terminal execution status."`
	ExitCode         *int                     `name:"exit-code" help:"Process exit code."`
	DurationMS       *int64                   `name:"duration-ms" help:"Process duration in milliseconds."`
	Signal           string                   `name:"signal" help:"Signal that ended the process."`
	TimedOut         bool                     `name:"timed-out" help:"Record that the process exceeded its timeout."`
	Database         string                   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool                     `name:"json" help:"Write a versioned JSON response."`
}

type RunValidationCommand struct {
	Record RunValidationRecordCommand `cmd:"" help:"Record a validation for a terminal execution."`
}

type RunValidationRecordCommand struct {
	RunID        string                    `name:"run" required:"" help:"Run ID used to enforce current-project scope."`
	ExecutionID  string                    `arg:"" help:"Canonical UUIDv7 execution ID."`
	ValidationID string                    `name:"validation-id" help:"Client-generated canonical UUIDv7 validation ID."`
	Source       runmodel.ValidationSource `name:"source" enum:"attested" required:"" help:"Manual validation source; must be attested."`
	Status       runmodel.ValidationStatus `name:"status" enum:"passed,failed" required:"" help:"Validation result."`
	Command      string                    `name:"command" required:"" help:"Executed command or validator name."`
	ExitCode     *int                      `name:"exit-code" help:"Validation exit code."`
	DurationMS   *int64                    `name:"duration-ms" help:"Validation duration in milliseconds."`
	Summary      string                    `name:"summary" required:"" help:"Validation summary."`
	Database     string                    `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON         bool                      `name:"json" help:"Write a versioned JSON response."`
}

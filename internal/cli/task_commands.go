package cli

import "github.com/vtimame/istok.sh/internal/task"

type TaskCommand struct {
	Create     TaskCreateCommand     `cmd:"" help:"Create a task in the current project."`
	List       TaskListCommand       `cmd:"" help:"List open tasks."`
	Show       TaskShowCommand       `cmd:"" help:"Show task details."`
	Ready      TaskReadyCommand      `cmd:"" help:"List tasks ready to claim."`
	Comment    TaskCommentCommand    `cmd:"" help:"Add a comment to a task."`
	Progress   TaskProgressCommand   `cmd:"" help:"Record task progress."`
	Dependency TaskDependencyCommand `cmd:"" help:"Manage task dependencies."`
	Claim      TaskClaimCommand      `cmd:"" help:"Claim a ready task and create an active run."`
	Done       TaskDoneCommand       `cmd:"" help:"Complete a task with validation evidence or an override."`
}

type TaskCreateCommand struct {
	Title              string `name:"title" required:"" help:"Task title."`
	Description        string `name:"description" help:"Task description."`
	AcceptanceCriteria string `name:"acceptance-criteria" help:"Task acceptance criteria."`
	Notes              string `name:"notes" help:"Task notes."`
	Database           string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON               bool   `name:"json" help:"Write a versioned JSON response."`
}

type TaskListCommand struct {
	Status   []task.Status `name:"status" enum:"open,blocked,done" help:"Task status filter; can be repeated."`
	Database string        `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool          `name:"json" help:"Write a versioned JSON response."`
}

type TaskShowCommand struct {
	ID       int64  `arg:"" name:"ID" required:"" help:"Project-scoped task number in the current project."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type TaskReadyCommand struct {
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type TaskCommentCommand struct {
	ID       int64  `arg:"" name:"TASK_NUMBER" required:"" help:"Project-scoped task number in the current project."`
	Body     string `name:"body" required:"" help:"Comment body."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type TaskProgressCommand struct {
	ID       int64  `arg:"" name:"TASK_NUMBER" required:"" help:"Project-scoped task number in the current project."`
	Body     string `name:"body" required:"" help:"Progress update body."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type TaskDependencyCommand struct {
	Add    TaskDependencyAddCommand    `cmd:"" help:"Add a blocker dependency."`
	Remove TaskDependencyRemoveCommand `cmd:"" help:"Remove a blocker dependency."`
}

type TaskDependencyAddCommand struct {
	ID       int64  `arg:"" name:"TASK_NUMBER" required:"" help:"Project-scoped blocked task number in the current project."`
	Blocker  int64  `name:"blocker" required:"" help:"Project-scoped blocking task number."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type TaskDependencyRemoveCommand struct {
	ID       int64  `arg:"" name:"TASK_NUMBER" required:"" help:"Project-scoped blocked task number in the current project."`
	Blocker  int64  `name:"blocker" required:"" help:"Project-scoped blocking task number."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type TaskClaimCommand struct {
	ID                      int64    `arg:"" name:"ID" required:"" help:"Project-scoped task number in the current project."`
	RunID                   string   `name:"run-id" help:"Client-generated canonical UUIDv7 run ID."`
	SnapshotID              string   `name:"snapshot-id" help:"Client-generated canonical UUIDv7 context snapshot ID."`
	ContextLimit            int      `name:"context-limit" help:"Explicit durable record limit (1-64); zero auto-expands for required always context."`
	ContextID               []string `name:"context-id" help:"Explicit context record UUIDv7 to include; can be repeated."`
	AllContext              bool     `name:"all-context" help:"Include all active context with an audited override."`
	ContextOverrideReason   string   `name:"context-override-reason" help:"Audited reason required with --all-context."`
	BaseBranch              string   `name:"base-branch" help:"Base Git branch; detected from the worktree when omitted."`
	BaseCommit              string   `name:"base-commit" help:"Base Git commit; detected from the worktree when omitted."`
	WithoutRetrieval        bool     `name:"without-retrieval" help:"Claim without local code retrieval; requires --override-reason."`
	RetrievalOverrideReason string   `name:"override-reason" help:"Audited reason for --without-retrieval."`
	Database                string   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON                    bool     `name:"json" help:"Write a versioned JSON response."`
}

type TaskDoneCommand struct {
	ID               int64  `arg:"" name:"ID" required:"" help:"Project-scoped task number in the current project."`
	ExpectedRevision int64  `name:"expected-revision" required:"" help:"Current task revision required for compare-and-swap."`
	CompletionID     string `name:"completion-id" help:"Client-generated canonical UUIDv7 completion ID."`
	RunID            string `name:"run-id" help:"Succeeded evidence run ID."`
	ValidationID     string `name:"validation-id" help:"Passed validation ID from the evidence run."`
	Note             string `name:"note" required:"" help:"Completion note."`
	OverrideReason   string `name:"override-reason" help:"Audited reason for completing without evidence."`
	Database         string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON             bool   `name:"json" help:"Write a versioned JSON response."`
}

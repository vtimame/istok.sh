package mcpserver

import (
	"github.com/vtimame/istok.sh/internal/task"
)

const taskSchemaVersion = "1"

type TaskResult struct {
	SchemaVersion string     `json:"schema_version"`
	Task          *task.Task `json:"task,omitempty"`
	Error         *ToolError `json:"error,omitempty"`
}

type TaskListResult struct {
	SchemaVersion string              `json:"schema_version"`
	Tasks         []task.TaskListItem `json:"tasks"`
	Error         *ToolError          `json:"error,omitempty"`
}

type TaskShowResult struct {
	SchemaVersion string     `json:"schema_version"`
	Show          *task.Show `json:"show,omitempty"`
	Error         *ToolError `json:"error,omitempty"`
}

type taskCreateInput struct {
	TaskID             string `json:"task_id" jsonschema:"Client-generated canonical UUIDv7 task identifier."`
	Title              string `json:"title" jsonschema:"Task title; must not be blank."`
	Description        string `json:"description,omitempty" jsonschema:"Optional task description."`
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty" jsonschema:"Optional acceptance criteria."`
	Notes              string `json:"notes,omitempty" jsonschema:"Optional task notes."`
}

type taskUpdateInput struct {
	TaskID             string  `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	ExpectedRevision   int64   `json:"expected_revision" jsonschema:"Positive current task revision required for compare-and-swap."`
	Title              *string `json:"title,omitempty" jsonschema:"Optional replacement title; must not be blank when provided."`
	Description        *string `json:"description,omitempty" jsonschema:"Optional replacement description."`
	AcceptanceCriteria *string `json:"acceptance_criteria,omitempty" jsonschema:"Optional replacement acceptance criteria."`
	Notes              *string `json:"notes,omitempty" jsonschema:"Optional replacement notes."`
}

type taskListInput struct {
	Statuses       []task.Status `json:"statuses,omitempty" jsonschema:"Optional statuses to include: open, blocked, done."`
	IncludeDeleted bool          `json:"include_deleted,omitempty" jsonschema:"Include soft-deleted tasks."`
}

type taskShowInput struct {
	TaskID         string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	IncludeDeleted bool   `json:"include_deleted,omitempty" jsonschema:"Include a soft-deleted task."`
}

type taskTextInput struct {
	TaskID           string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current task revision required for compare-and-swap."`
	Body             string `json:"body" jsonschema:"Non-blank comment or progress body."`
}

type taskBlockInput struct {
	TaskID           string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current task revision required for compare-and-swap."`
	Reason           string `json:"reason" jsonschema:"Non-blank reason for blocking the task."`
}

type taskUnblockInput struct {
	TaskID           string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current task revision required for compare-and-swap."`
	Note             string `json:"note" jsonschema:"Non-blank note for unblocking the task."`
}

type taskDependencyInput struct {
	BlockerTaskID    string `json:"blocker_task_id" jsonschema:"Canonical UUIDv7 identifier of the blocking task."`
	BlockedTaskID    string `json:"blocked_task_id" jsonschema:"Canonical UUIDv7 identifier of the task being blocked."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current revision of blocked_task_id required for compare-and-swap."`
}

type taskRevisionInput struct {
	TaskID           string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier."`
	ExpectedRevision int64  `json:"expected_revision" jsonschema:"Positive current task revision required for compare-and-swap."`
}

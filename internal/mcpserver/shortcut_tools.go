package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	runapp "github.com/vtimame/istok.sh/internal/application/run"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type taskEvidenceInput struct {
	TaskID     string               `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier of a task you claimed."`
	Status     run.ValidationStatus `json:"status" jsonschema:"passed or failed."`
	Summary    string               `json:"summary" jsonschema:"What was checked and what it showed; must not be blank."`
	Command    string               `json:"command,omitempty" jsonschema:"Optional command, pipeline or check name. Omit it for a manual check."`
	ExitCode   *int                 `json:"exit_code,omitempty" jsonschema:"Optional exit code."`
	DurationMS *int64               `json:"duration_ms,omitempty" jsonschema:"Optional duration in milliseconds."`
	OutputTail string               `json:"output_tail,omitempty" jsonschema:"Optional end of the output; the last 4 KiB are kept."`
}

type taskDoneInput struct {
	TaskID            string `json:"task_id" jsonschema:"Canonical UUIDv7 task identifier of a task you claimed."`
	Summary           string `json:"summary" jsonschema:"Result summary; must not be blank."`
	ValidationID      string `json:"validation_id,omitempty" jsonschema:"Optional passed validation of your run. Omitted: the latest passed validation of the run is used."`
	NotVerifiedReason string `json:"not_verified_reason,omitempty" jsonschema:"Why the result could not be verified. Closes the task without a passed validation; the reason stays in its history."`
}

// EvidenceResult is the response of task_evidence.
type EvidenceResult struct {
	SchemaVersion string          `json:"schema_version"`
	Execution     *run.Execution  `json:"execution,omitempty"`
	Validation    *run.Validation `json:"validation,omitempty"`
	Error         *ToolError      `json:"error,omitempty"`
}

// TaskDoneResult is the response of task_done.
type TaskDoneResult struct {
	SchemaVersion string          `json:"schema_version"`
	Task          *task.Task      `json:"task,omitempty"`
	Run           *run.Run        `json:"run,omitempty"`
	Completion    *run.Completion `json:"completion,omitempty"`
	Error         *ToolError      `json:"error,omitempty"`
}

// addShortcutTools adds calls that work on the actor's own active run of a
// task, so the agent needs no run ID, lease ID or revision.
func addShortcutTools(server *mcp.Server, projects *project.Service, runs *runapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("task_evidence", "Record a check made outside Istok (a command, a CI pipeline or a manual test) as an attested validation of your run of the task, in one call.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in taskEvidenceInput) (*mcp.CallToolResult, EvidenceResult, error) {
		selector, err := currentTaskSelector(ctx, projects, root, in.TaskID)
		if err != nil {
			return runErrorTool(err), EvidenceResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}, nil
		}

		evidence, err := runs.RecordTaskEvidence(ctx, selector, runapp.TaskEvidenceInput{
			Status:     in.Status,
			Summary:    in.Summary,
			Command:    in.Command,
			ExitCode:   in.ExitCode,
			DurationMS: in.DurationMS,
			OutputTail: in.OutputTail,
			CWD:        root,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), EvidenceResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}, nil
		}

		return nil, EvidenceResult{SchemaVersion: runSchemaVersion, Execution: &evidence.Execution, Validation: &evidence.Validation}, nil
	})

	mcp.AddTool(server, tool("task_done", "Finish your run of the task and complete the task in one call. Uses a passed validation of the run, or closes it unverified with not_verified_reason.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in taskDoneInput) (*mcp.CallToolResult, TaskDoneResult, error) {
		selector, err := currentTaskSelector(ctx, projects, root, in.TaskID)
		if err != nil {
			return runErrorTool(err), TaskDoneResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}, nil
		}

		done, err := runs.DoneTask(ctx, selector, runapp.TaskDoneInput{
			Summary:           in.Summary,
			ValidationID:      in.ValidationID,
			NotVerifiedReason: in.NotVerifiedReason,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), TaskDoneResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}, nil
		}

		return nil, TaskDoneResult{SchemaVersion: runSchemaVersion, Task: &done.Task, Run: &done.Run, Completion: &done.Completion}, nil
	})
}

func currentTaskSelector(ctx context.Context, projects *project.Service, root, taskID string) (task.Selector, error) {
	current, err := currentTaskProject(ctx, projects, root)
	if err != nil {
		return task.Selector{}, err
	}

	return taskSelector(current.ID, taskID)
}

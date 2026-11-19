package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	handoffapp "github.com/vtimame/istok.sh/internal/application/handoff"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type taskStartInput struct {
	TaskID             string `json:"task_id,omitempty" jsonschema:"Canonical UUIDv7 task identifier of an existing task. Omit it and pass title to create the task."`
	Title              string `json:"title,omitempty" jsonschema:"Title of a new task; used only without task_id."`
	Description        string `json:"description,omitempty" jsonschema:"Optional description of a new task."`
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty" jsonschema:"Optional acceptance criteria of a new task."`
	ContextLimit       int    `json:"context_limit,omitempty" jsonschema:"Maximum number of context records to include in a new run snapshot."`
}

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

// TaskStartResult is the response of task_start.
type TaskStartResult struct {
	SchemaVersion string               `json:"schema_version"`
	Task          *task.Task           `json:"task,omitempty"`
	Run           *run.Run             `json:"run,omitempty"`
	Resumed       bool                 `json:"resumed"`
	Notice        string               `json:"notice,omitempty"`
	Snapshot      *run.ContextSnapshot `json:"snapshot,omitempty"`
	Handoff       *handoffapp.Brief    `json:"handoff,omitempty"`
	Error         *ToolError           `json:"error,omitempty"`
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
func addShortcutTools(server *mcp.Server, projects *project.Service, tasks *taskapp.Service, runs *runapp.Service, root string, actor actorIdentity) {
	briefs := handoffapp.NewService(tasks, runs)

	mcp.AddTool(server, tool("task_start", "Start work on a task in one call: create it from title when task_id is omitted, claim it or continue your own active run of it, and return the run, its context snapshot and the handoff (latest handoff note, recent notes, earlier runs). Safe to call again after a restart or a context compaction.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in taskStartInput) (*mcp.CallToolResult, TaskStartResult, error) {
		failed := func(err error) (*mcp.CallToolResult, TaskStartResult, error) {
			return runErrorTool(err), TaskStartResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}, nil
		}

		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return failed(err)
		}

		taskID, err := startTaskID(ctx, tasks, current.ID, in, actor)
		if err != nil {
			return failed(err)
		}
		selector, err := taskSelector(current.ID, taskID)
		if err != nil {
			return failed(err)
		}

		started, resumed, err := runs.StartTask(ctx, selector, run.ClaimInput{ContextLimit: in.ContextLimit}, actor.run())
		if err != nil {
			return failed(err)
		}

		show, err := runs.ShowRun(ctx, started.ID)
		if err != nil {
			return failed(err)
		}
		brief, err := briefs.ForTask(ctx, selector)
		if err != nil {
			return failed(err)
		}

		result := TaskStartResult{
			SchemaVersion: runSchemaVersion,
			Task:          &brief.Task,
			Run:           &started,
			Resumed:       resumed,
			Snapshot:      &show.Snapshot,
			Handoff:       &brief,
		}
		if brief.ActiveRun != nil && brief.ActiveRun.LeaseExpired {
			result.Notice = "the lease expired while an execution was running; call run_recover with a reason before continuing"
		}

		return nil, result, nil
	})

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

// startTaskID returns the task to start, creating it from the title when the
// caller passed no task ID.
func startTaskID(ctx context.Context, tasks *taskapp.Service, projectID string, in taskStartInput, actor actorIdentity) (string, error) {
	if in.TaskID != "" {
		return in.TaskID, nil
	}
	if in.Title == "" {
		return "", task.NewError(task.CodeInvalid, "pass task_id of an existing task or title of a new one")
	}

	id, err := task.NewID()
	if err != nil {
		return "", err
	}

	created, err := tasks.Create(ctx, task.CreateInput{
		ID:                 id,
		ProjectID:          projectID,
		Title:              in.Title,
		Description:        in.Description,
		AcceptanceCriteria: in.AcceptanceCriteria,
	}, actor.task())
	if err != nil {
		return "", err
	}

	return created.ID, nil
}

func currentTaskSelector(ctx context.Context, projects *project.Service, root, taskID string) (task.Selector, error) {
	current, err := currentTaskProject(ctx, projects, root)
	if err != nil {
		return task.Selector{}, err
	}

	return taskSelector(current.ID, taskID)
}

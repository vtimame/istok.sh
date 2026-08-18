package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	runapp "s26.dev/istok-cli/internal/application/run"
	runworkflow "s26.dev/istok-cli/internal/application/runworkflow"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

func addRunTools(server *mcp.Server, projects *project.Service, runs *runapp.Service, workflow *runworkflow.Service, root string, actor actorIdentity, profile Profile) {
	mcp.AddTool(server, tool("task_claim", "Claim an active task into a run in the current project.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in runClaimInput) (*mcp.CallToolResult, RunResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		selector, err := taskSelector(current.ID, in.TaskID)
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		value, err := runs.Claim(ctx, selector, run.ClaimInput{
			ID:                      in.RunID,
			SnapshotID:              in.SnapshotID,
			ContextLimit:            in.ContextLimit,
			BaseBranch:              in.BaseBranch,
			BaseCommit:              in.BaseCommit,
			WithoutRetrieval:        in.WithoutRetrieval,
			RetrievalOverrideReason: in.RetrievalOverrideReason,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		show, err := runs.ShowRun(ctx, value.ID)
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		result := runResult(&value)
		result.Snapshot = &show.Snapshot
		return nil, result, nil
	})

	mcp.AddTool(server, tool("run_list", "List runs in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in runListInput) (*mcp.CallToolResult, RunListResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return runErrorTool(err), runListErrorResult(err), nil
		}
		values, err := runs.ListRuns(ctx, run.ListOptions{
			ProjectID: current.ID,
			TaskID:    in.TaskID,
			Statuses:  in.Statuses,
			Limit:     in.Limit,
		})
		if err != nil {
			return runErrorTool(err), runListErrorResult(err), nil
		}
		return nil, runListResult(values, nil), nil
	})

	mcp.AddTool(server, tool("run_show", "Show a run in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in runShowInput) (*mcp.CallToolResult, RunShowResult, error) {
		_, value, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), runShowErrorResult(err), nil
		}
		return nil, runShowResult(value), nil
	})

	mcp.AddTool(server, tool("execution_start", "Start an execution for a scoped run.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in executionStartInput) (*mcp.CallToolResult, ExecutionResult, error) {
		_, _, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), executionErrorResult(err), nil
		}
		value, err := runs.StartExecution(ctx, run.StartExecutionInput{
			ID:      in.ExecutionID,
			RunID:   in.RunID,
			LeaseID: in.LeaseID,
			Argv:    in.Argv,
			CWD:     in.CWD,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), executionErrorResult(err), nil
		}
		return nil, executionResult(&value), nil
	})

	mcp.AddTool(server, tool("execution_finish", "Finish an execution for a scoped run.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in executionFinishInput) (*mcp.CallToolResult, ExecutionResult, error) {
		_, show, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), executionErrorResult(err), nil
		}
		if _, err := runExecutionInShow(show, in.ExecutionID); err != nil {
			return runErrorTool(err), executionErrorResult(err), nil
		}
		value, err := runs.FinishExecution(ctx, run.FinishExecutionInput{
			ExecutionID:      in.ExecutionID,
			ExpectedRevision: in.ExpectedRevision,
			Status:           in.Status,
			ExitCode:         in.ExitCode,
			DurationMS:       in.DurationMS,
			Signal:           in.Signal,
			TimedOut:         in.TimedOut,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), executionErrorResult(err), nil
		}
		return nil, executionResult(&value), nil
	})

	mcp.AddTool(server, tool("validation_record", "Record validation for an execution in a scoped run.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in validationRecordInput) (*mcp.CallToolResult, ValidationResult, error) {
		_, show, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), validationErrorResult(err), nil
		}
		if _, err := runExecutionInShow(show, in.ExecutionID); err != nil {
			return runErrorTool(err), validationErrorResult(err), nil
		}
		value, err := runs.RecordValidation(ctx, run.RecordValidationInput{
			ID:          in.ValidationID,
			ExecutionID: in.ExecutionID,
			Source:      in.Source,
			Status:      in.Status,
			Command:     in.Command,
			ExitCode:    in.ExitCode,
			DurationMS:  in.DurationMS,
			Summary:     in.Summary,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), validationErrorResult(err), nil
		}
		return nil, validationResult(&value), nil
	})

	mcp.AddTool(server, tool("run_finish", "Finish a scoped run.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in runFinishInput) (*mcp.CallToolResult, RunResult, error) {
		_, _, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		allowUnvalidated := false
		if in.AllowUnvalidated != nil {
			allowUnvalidated = *in.AllowUnvalidated
		}
		value, err := runs.FinishRun(ctx, run.FinishRunInput{
			RunID:            in.RunID,
			LeaseID:          in.LeaseID,
			ExpectedRevision: in.ExpectedRevision,
			Status:           in.Status,
			ResultSummary:    in.ResultSummary,
			AllowUnvalidated: allowUnvalidated,
			OverrideReason:   in.OverrideReason,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		return nil, runResult(&value), nil
	})

	mcp.AddTool(server, tool("task_complete", "Complete a task in the current project using run completion evidence.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in taskCompleteInput) (*mcp.CallToolResult, CompletionResult, error) {
		current, err := currentTaskProject(ctx, projects, root)
		if err != nil {
			return runErrorTool(err), completionErrorResult(err), nil
		}
		selector, err := taskSelector(current.ID, in.TaskID)
		if err != nil {
			return runErrorTool(err), completionErrorResult(err), nil
		}
		taskValue, completion, err := runs.CompleteTask(ctx, selector, run.CompleteTaskInput{
			ID:                   in.CompletionID,
			ExpectedTaskRevision: in.ExpectedTaskRevision,
			RunID:                in.RunID,
			ValidationID:         in.ValidationID,
			Note:                 in.Note,
			OverrideReason:       in.OverrideReason,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), completionErrorResult(err), nil
		}
		return nil, completionResult(taskValue, completion), nil
	})

	addManagedRunTools(server, projects, runs, workflow, root, actor, profile)
}

func addManagedRunTools(server *mcp.Server, projects *project.Service, runs *runapp.Service, workflow *runworkflow.Service, root string, actor actorIdentity, profile Profile) {
	addManagedExecutionTool(server, "run_exec", "Execute a managed command for a scoped run.", projects, runs, workflow, root, actor, false)
	addManagedExecutionTool(server, "run_validate", "Execute a managed validation command for a scoped run.", projects, runs, workflow, root, actor, true)

	mcp.AddTool(server, tool("run_heartbeat", "Extend the current scoped run lease.", false, false, false), func(ctx context.Context, _ *mcp.CallToolRequest, in runHeartbeatInput) (*mcp.CallToolResult, RunResult, error) {
		_, _, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		value, err := runs.Heartbeat(ctx, run.HeartbeatInput{RunID: in.RunID, LeaseID: in.LeaseID}, actor.run())
		if err != nil {
			return runErrorTool(err), runErrorResult(err), nil
		}
		return nil, runResult(&value), nil
	})

	mcp.AddTool(server, tool("run_artifact_list", "List managed artifacts for a scoped run.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in runArtifactListInput) (*mcp.CallToolResult, ArtifactListResult, error) {
		_, show, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), artifactListErrorResult(err), nil
		}
		if show.Artifacts == nil {
			show.Artifacts = []run.Artifact{}
		}
		return nil, ArtifactListResult{SchemaVersion: runSchemaVersion, Artifacts: show.Artifacts}, nil
	})

	mcp.AddTool(server, tool("run_artifact_verify", "Verify a managed artifact for a scoped run.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in runArtifactVerifyInput) (*mcp.CallToolResult, ArtifactVerifyResult, error) {
		_, show, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
		if err != nil {
			return runErrorTool(err), artifactVerifyErrorResult(err), nil
		}
		if !artifactInRun(show, in.ArtifactID) {
			err := run.NewError(run.CodeNotFound, "artifact was not found in current run")
			return runErrorTool(err), artifactVerifyErrorResult(err), nil
		}
		artifact, err := workflow.VerifyArtifact(ctx, in.ArtifactID)
		if err != nil {
			return runErrorTool(err), artifactVerifyErrorResult(err), nil
		}
		return nil, ArtifactVerifyResult{SchemaVersion: runSchemaVersion, Artifact: &artifact, Verified: true}, nil
	})

	if profile == Worker || profile == Supervisor || profile == Admin {
		mcp.AddTool(server, tool("run_recover", "Recover a scoped run lease.", false, true, false), func(ctx context.Context, _ *mcp.CallToolRequest, in runRecoverInput) (*mcp.CallToolResult, RunResult, error) {
			_, _, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID)
			if err != nil {
				return runErrorTool(err), runErrorResult(err), nil
			}
			value, err := runs.Recover(ctx, run.RecoverInput{RunID: in.RunID, Force: in.Force, Reason: in.Reason}, actor.run())
			if err != nil {
				return runErrorTool(err), runErrorResult(err), nil
			}
			return nil, runResult(&value), nil
		})
	}
	if profile == Supervisor || profile == Admin {
		mcp.AddTool(server, tool("run_abandon", "Abandon a scoped active run.", false, true, false), func(ctx context.Context, _ *mcp.CallToolRequest, in runAbandonInput) (*mcp.CallToolResult, RunResult, error) {
			if _, _, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID); err != nil {
				return runErrorTool(err), runErrorResult(err), nil
			}
			value, err := runs.Abandon(ctx, run.AbandonInput{RunID: in.RunID, ExpectedRevision: in.ExpectedRevision, Reason: in.Reason}, actor.run())
			if err != nil {
				return runErrorTool(err), runErrorResult(err), nil
			}
			return nil, runResult(&value), nil
		})
	}
}

func addManagedExecutionTool(server *mcp.Server, name, description string, projects *project.Service, runs *runapp.Service, workflow *runworkflow.Service, root string, actor actorIdentity, validate bool) {
	mcp.AddTool(server, tool(name, description, false, true, false), func(ctx context.Context, _ *mcp.CallToolRequest, in managedExecutionInput) (*mcp.CallToolResult, ManagedExecutionResult, error) {
		if err := in.validate(); err != nil {
			return runErrorTool(err), managedExecutionErrorResult(err), nil
		}
		if _, _, err := runShowInCurrentProject(ctx, projects, runs, root, in.RunID); err != nil {
			return runErrorTool(err), managedExecutionErrorResult(err), nil
		}
		cwd := root
		if in.CWD != "" {
			cwd = filepath.Join(root, in.CWD)
		}

		result, err := workflow.Execute(ctx, runworkflow.ExecuteInput{
			RunID:            in.RunID,
			LeaseID:          in.LeaseID,
			ProjectRoot:      root,
			CWD:              cwd,
			Argv:             in.Argv,
			Environment:      os.Environ(),
			Timeout:          durationOr(in.TimeoutMS, defaultManagedTimeout(validate)),
			TerminationGrace: durationOr(in.TerminationGraceMS, 5*time.Second),
			OutputLimit:      outputLimitOr(in.OutputLimit),
			Validate:         validate,
			AllowDangerous:   in.AllowDangerous,
			DangerousReason:  in.OverrideReason,
		}, actor.run())
		if err != nil {
			return runErrorTool(err), managedExecutionErrorResult(err), nil
		}
		if result.Artifacts == nil {
			result.Artifacts = []run.Artifact{}
		}
		return nil, ManagedExecutionResult{SchemaVersion: runSchemaVersion, Execution: &result.Execution, Artifacts: result.Artifacts, Validation: result.Validation}, nil
	})
}

func defaultManagedTimeout(validate bool) time.Duration {
	if validate {
		return 10 * time.Minute
	}
	return 30 * time.Minute
}
func durationOr(value *int64, fallback time.Duration) time.Duration {
	if value == nil {
		return fallback
	}

	return time.Duration(*value) * time.Millisecond
}
func outputLimitOr(value *int64) int64 {
	if value == nil {
		return 1 << 20
	}

	return *value
}
func artifactInRun(show run.Show, id string) bool {
	for _, artifact := range show.Artifacts {
		if artifact.ID == id {
			return true
		}
	}
	return false
}

func runShowInCurrentProject(ctx context.Context, projects *project.Service, runs *runapp.Service, root, runID string) (project.Project, run.Show, error) {
	current, err := currentTaskProject(ctx, projects, root)
	if err != nil {
		return project.Project{}, run.Show{}, err
	}
	value, err := runs.ShowRun(ctx, runID)
	if err != nil {
		return project.Project{}, run.Show{}, err
	}
	if value.Snapshot.ProjectID != current.ID {
		return project.Project{}, run.Show{}, run.NewError(run.CodeNotFound, "run was not found")
	}

	if value.Executions == nil {
		value.Executions = []run.Execution{}
	}
	if value.Validations == nil {
		value.Validations = []run.Validation{}
	}
	if value.Artifacts == nil {
		value.Artifacts = []run.Artifact{}
	}

	return current, value, nil
}

func runExecutionInShow(value run.Show, executionID string) (run.Execution, error) {
	for _, item := range value.Executions {
		if item.ID == executionID {
			return item, nil
		}
	}
	return run.Execution{}, run.NewError(run.CodeNotFound, "execution was not found in current run")
}

func runResult(value *run.Run) RunResult {
	return RunResult{SchemaVersion: runSchemaVersion, Run: value}
}

func runErrorResult(err error) RunResult {
	return RunResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}
}

func runListErrorResult(err error) RunListResult {
	return runListResult(nil, toolError(err))
}

func runListResult(values []run.Run, toolErr *ToolError) RunListResult {
	if values == nil {
		values = []run.Run{}
	}

	return RunListResult{SchemaVersion: runSchemaVersion, Runs: values, Error: toolErr}
}

func runShowErrorResult(err error) RunShowResult {
	return RunShowResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}
}

func runShowResult(value run.Show) RunShowResult {
	if value.Executions == nil {
		value.Executions = []run.Execution{}
	}
	if value.Validations == nil {
		value.Validations = []run.Validation{}
	}
	if value.Artifacts == nil {
		value.Artifacts = []run.Artifact{}
	}

	return RunShowResult{SchemaVersion: runSchemaVersion, Show: &value}
}

func executionErrorResult(err error) ExecutionResult {
	return ExecutionResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}
}

func executionResult(value *run.Execution) ExecutionResult {
	return ExecutionResult{SchemaVersion: runSchemaVersion, Execution: value}
}

func validationErrorResult(err error) ValidationResult {
	return ValidationResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}
}

func validationResult(value *run.Validation) ValidationResult {
	return ValidationResult{SchemaVersion: runSchemaVersion, Validation: value}
}

func completionErrorResult(err error) CompletionResult {
	return CompletionResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}
}

func completionResult(taskValue task.Task, completionValue run.Completion) CompletionResult {
	return CompletionResult{
		SchemaVersion: runSchemaVersion,
		Task:          &taskValue,
		Completion:    &completionValue,
	}
}

func managedExecutionErrorResult(err error) ManagedExecutionResult {
	return ManagedExecutionResult{SchemaVersion: runSchemaVersion, Artifacts: []run.Artifact{}, Error: toolError(err)}
}

func artifactListErrorResult(err error) ArtifactListResult {
	return ArtifactListResult{SchemaVersion: runSchemaVersion, Artifacts: []run.Artifact{}, Error: toolError(err)}
}

func artifactVerifyErrorResult(err error) ArtifactVerifyResult {
	return ArtifactVerifyResult{SchemaVersion: runSchemaVersion, Error: toolError(err)}
}

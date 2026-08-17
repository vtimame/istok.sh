package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/fx"

	"s26.dev/istok-cli/internal/application/bootstrap"
	runapp "s26.dev/istok-cli/internal/application/run"
	runworkflow "s26.dev/istok-cli/internal/application/runworkflow"
	taskapp "s26.dev/istok-cli/internal/application/task"
	"s26.dev/istok-cli/internal/cli/presentation"
	"s26.dev/istok-cli/internal/project"
	runmodel "s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

const runSchemaVersion = "1"

var cliRunActor = runmodel.ActorSnapshot{ID: "cli", Kind: "cli", Name: "CLI"}

type runKernel struct {
	projects *project.Service
	tasks    *taskapp.Service
	runs     *runapp.Service
}

type runWorkflowKernel struct {
	runKernel
	workflow *runworkflow.Service
}

type artifactVerificationResult struct {
	Artifact runmodel.Artifact `json:"artifact"`
	Verified bool              `json:"verified"`
}

type runListItem struct {
	runmodel.Run
	TaskNumber int64 `json:"task_number"`
}

type runShowView struct {
	runmodel.Show
	TaskNumber int64 `json:"task_number"`
}

type taskCompletionResult struct {
	Task       task.Task           `json:"task"`
	Completion runmodel.Completion `json:"completion"`
}

func runTaskClaim(ctx context.Context, command TaskClaimCommand, cwd string, output io.Writer) error {
	baseBranch, baseCommit := command.BaseBranch, command.BaseCommit
	if baseBranch == "" {
		baseBranch = gitValue(ctx, cwd, "symbolic-ref", "--quiet", "--short", "HEAD")
	}
	if baseCommit == "" {
		baseCommit = gitValue(ctx, cwd, "rev-parse", "HEAD")
	}

	return runKernelCommand(ctx, command.Database, command.JSON, output, func(kernel runKernel) (any, error) {
		current, err := kernel.projects.Current(ctx, cwd)
		if err != nil {
			return nil, err
		}

		return kernel.runs.Claim(ctx, task.Selector{ProjectID: current.ID, Number: command.ID}, runmodel.ClaimInput{
			ID:                      command.RunID,
			SnapshotID:              command.SnapshotID,
			ContextLimit:            command.ContextLimit,
			BaseBranch:              baseBranch,
			BaseCommit:              baseCommit,
			WithoutRetrieval:        command.WithoutRetrieval,
			RetrievalOverrideReason: command.RetrievalOverrideReason,
		}, cliRunActor)
	})
}

func runTaskDone(ctx context.Context, command TaskDoneCommand, cwd string, output io.Writer) error {
	return runKernelCommand(ctx, command.Database, command.JSON, output, func(kernel runKernel) (any, error) {
		current, err := kernel.projects.Current(ctx, cwd)
		if err != nil {
			return nil, err
		}

		completed, completion, err := kernel.runs.CompleteTask(ctx, task.Selector{
			ProjectID: current.ID,
			Number:    command.ID,
		}, runmodel.CompleteTaskInput{
			ID:                   command.CompletionID,
			ExpectedTaskRevision: command.ExpectedRevision,
			RunID:                command.RunID,
			ValidationID:         command.ValidationID,
			Note:                 command.Note,
			OverrideReason:       command.OverrideReason,
		}, cliRunActor)
		if err != nil {
			return nil, err
		}

		return taskCompletionResult{Task: completed, Completion: completion}, nil
	})
}

func runRunCommand(ctx context.Context, command RunCommand, commandName, cwd string, output io.Writer) error {
	switch {
	case commandName == "run list":
		return runKernelCommand(ctx, command.List.Database, command.List.JSON, output, func(kernel runKernel) (any, error) {
			current, err := kernel.projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}

			options := runmodel.ListOptions{ProjectID: current.ID, Statuses: command.List.Status, Limit: command.List.Limit}
			if command.List.Task > 0 {
				shown, err := kernel.tasks.Show(ctx, task.Selector{ProjectID: current.ID, Number: command.List.Task}, false)
				if err != nil {
					return nil, err
				}
				options.TaskID = shown.Task.ID
			}

			values, err := kernel.runs.ListRuns(ctx, options)
			if err != nil {
				return nil, err
			}

			result := make([]runListItem, 0, len(values))
			for _, value := range values {
				shown, err := kernel.tasks.Show(ctx, task.Selector{ProjectID: current.ID, ID: value.TaskID}, false)
				if err != nil {
					return nil, err
				}

				result = append(result, runListItem{Run: value, TaskNumber: shown.Task.Number})
			}

			return result, nil
		})
	case strings.HasPrefix(commandName, "run show"):
		return runKernelCommand(ctx, command.Show.Database, command.Show.JSON, output, func(kernel runKernel) (any, error) {
			shown, err := scopedRun(ctx, kernel, cwd, command.Show.ID)
			if err != nil {
				return nil, err
			}
			current, err := kernel.projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			taskView, err := kernel.tasks.Show(ctx, task.Selector{ProjectID: current.ID, ID: shown.Run.TaskID}, false)
			if err != nil {
				return nil, err
			}

			return runShowView{Show: shown, TaskNumber: taskView.Task.Number}, nil
		})
	case commandName == "run exec" || strings.HasPrefix(commandName, "run exec "):
		return runManagedCommand(ctx, command.Exec.Database, command.Exec.JSON, output, func(kernel runWorkflowKernel) (any, error) {
			return executeManaged(ctx, kernel, cwd, command.Exec.ID, command.Exec.LeaseID, command.Exec.CWD, command.Exec.Argv, command.Exec.Timeout, command.Exec.TerminationGrace, command.Exec.OutputLimit, command.Exec.AllowDangerous, command.Exec.OverrideReason, false, "")
		})
	case strings.HasPrefix(commandName, "run validate"):
		return runManagedCommand(ctx, command.Validate.Database, command.Validate.JSON, output, func(kernel runWorkflowKernel) (any, error) {
			return executeManaged(ctx, kernel, cwd, command.Validate.ID, command.Validate.LeaseID, command.Validate.CWD, command.Validate.Argv, command.Validate.Timeout, command.Validate.TerminationGrace, command.Validate.OutputLimit, command.Validate.AllowDangerous, command.Validate.OverrideReason, true, command.Validate.Summary)
		})
	case strings.HasPrefix(commandName, "run heartbeat"):
		return runKernelCommand(ctx, command.Heartbeat.Database, command.Heartbeat.JSON, output, func(kernel runKernel) (any, error) {
			if _, err := scopedRun(ctx, kernel, cwd, command.Heartbeat.ID); err != nil {
				return nil, err
			}

			return kernel.runs.Heartbeat(ctx, runmodel.HeartbeatInput{
				RunID:   command.Heartbeat.ID,
				LeaseID: command.Heartbeat.LeaseID,
			}, cliRunActor)
		})
	case strings.HasPrefix(commandName, "run recover"):
		return runKernelCommand(ctx, command.Recover.Database, command.Recover.JSON, output, func(kernel runKernel) (any, error) {
			if _, err := scopedRun(ctx, kernel, cwd, command.Recover.ID); err != nil {
				return nil, err
			}

			return kernel.runs.Recover(ctx, runmodel.RecoverInput{
				RunID:  command.Recover.ID,
				Force:  command.Recover.Force,
				Reason: command.Recover.Reason,
			}, cliRunActor)
		})
	case strings.HasPrefix(commandName, "run artifact list"):
		return runKernelCommand(ctx, command.Artifact.List.Database, command.Artifact.List.JSON, output, func(kernel runKernel) (any, error) {
			shown, err := scopedRun(ctx, kernel, cwd, command.Artifact.List.RunID)
			if err != nil {
				return nil, err
			}

			return shown.Artifacts, nil
		})
	case strings.HasPrefix(commandName, "run artifact verify"):
		return runManagedCommand(ctx, command.Artifact.Verify.Database, command.Artifact.Verify.JSON, output, func(kernel runWorkflowKernel) (any, error) {
			shown, err := scopedRun(ctx, kernel.runKernel, cwd, command.Artifact.Verify.RunID)
			if err != nil {
				return nil, err
			}
			if !showContainsArtifact(shown, command.Artifact.Verify.ArtifactID) {
				return nil, runmodel.NewError(runmodel.CodeNotFound, "artifact was not found in the scoped run")
			}

			artifact, err := kernel.workflow.VerifyArtifact(ctx, command.Artifact.Verify.ArtifactID)
			if err != nil {
				return nil, err
			}

			return artifactVerificationResult{Artifact: artifact, Verified: true}, nil
		})
	case strings.HasPrefix(commandName, "run finish"):
		return runKernelCommand(ctx, command.Finish.Database, command.Finish.JSON, output, func(kernel runKernel) (any, error) {
			if _, err := scopedRun(ctx, kernel, cwd, command.Finish.ID); err != nil {
				return nil, err
			}

			return kernel.runs.FinishRun(ctx, runmodel.FinishRunInput{
				RunID:            command.Finish.ID,
				LeaseID:          command.Finish.LeaseID,
				ExpectedRevision: command.Finish.ExpectedRevision,
				Status:           command.Finish.Status,
				ResultSummary:    command.Finish.Summary,
				AllowUnvalidated: command.Finish.AllowUnvalidated,
				OverrideReason:   command.Finish.OverrideReason,
			}, cliRunActor)
		})
	case strings.HasPrefix(commandName, "run execution start"):
		return runKernelCommand(ctx, command.Execution.Start.Database, command.Execution.Start.JSON, output, func(kernel runKernel) (any, error) {
			if _, err := scopedRun(ctx, kernel, cwd, command.Execution.Start.RunID); err != nil {
				return nil, err
			}

			return kernel.runs.StartExecution(ctx, runmodel.StartExecutionInput{
				ID:      command.Execution.Start.ExecutionID,
				RunID:   command.Execution.Start.RunID,
				LeaseID: command.Execution.Start.LeaseID,
				Argv:    command.Execution.Start.Argv,
				CWD:     resolvePath(cwd, command.Execution.Start.CWD),
			}, cliRunActor)
		})
	case strings.HasPrefix(commandName, "run execution finish"):
		return runKernelCommand(ctx, command.Execution.Finish.Database, command.Execution.Finish.JSON, output, func(kernel runKernel) (any, error) {
			shown, err := scopedRun(ctx, kernel, cwd, command.Execution.Finish.RunID)
			if err != nil {
				return nil, err
			}
			if !showContainsExecution(shown, command.Execution.Finish.ExecutionID) {
				return nil, runmodel.NewError(runmodel.CodeNotFound, "execution was not found in the scoped run")
			}

			return kernel.runs.FinishExecution(ctx, runmodel.FinishExecutionInput{
				ExecutionID:      command.Execution.Finish.ExecutionID,
				ExpectedRevision: command.Execution.Finish.ExpectedRevision,
				Status:           command.Execution.Finish.Status,
				ExitCode:         command.Execution.Finish.ExitCode,
				DurationMS:       command.Execution.Finish.DurationMS,
				Signal:           command.Execution.Finish.Signal,
				TimedOut:         command.Execution.Finish.TimedOut,
			}, cliRunActor)
		})
	case strings.HasPrefix(commandName, "run validation record"):
		return runKernelCommand(ctx, command.Validation.Record.Database, command.Validation.Record.JSON, output, func(kernel runKernel) (any, error) {
			shown, err := scopedRun(ctx, kernel, cwd, command.Validation.Record.RunID)
			if err != nil {
				return nil, err
			}
			if !showContainsExecution(shown, command.Validation.Record.ExecutionID) {
				return nil, runmodel.NewError(runmodel.CodeNotFound, "execution was not found in the scoped run")
			}

			return kernel.runs.RecordValidation(ctx, runmodel.RecordValidationInput{
				ID:          command.Validation.Record.ValidationID,
				ExecutionID: command.Validation.Record.ExecutionID,
				Source:      command.Validation.Record.Source,
				Status:      command.Validation.Record.Status,
				Command:     command.Validation.Record.Command,
				ExitCode:    command.Validation.Record.ExitCode,
				DurationMS:  command.Validation.Record.DurationMS,
				Summary:     command.Validation.Record.Summary,
			}, cliRunActor)
		})
	default:
		return fmt.Errorf("unsupported run command %q", commandName)
	}
}

func executeManaged(
	ctx context.Context,
	kernel runWorkflowKernel,
	cwd, runID, leaseID, commandCWD string,
	argv []string,
	timeout, terminationGrace time.Duration,
	outputLimit int64,
	allowDangerous bool,
	overrideReason string,
	validate bool,
	validationSummary string,
) (any, error) {
	if _, err := scopedRun(ctx, kernel.runKernel, cwd, runID); err != nil {
		return nil, err
	}
	current, err := kernel.projects.Current(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if current.Root == nil {
		return nil, &project.Error{Code: project.CodeConflict, Message: "current project has no active root"}
	}

	return kernel.workflow.Execute(ctx, runworkflow.ExecuteInput{
		RunID:             runID,
		LeaseID:           leaseID,
		ProjectRoot:       current.Root.CanonicalPath,
		CWD:               resolvePath(cwd, commandCWD),
		Argv:              argv,
		Environment:       os.Environ(),
		Timeout:           timeout,
		TerminationGrace:  terminationGrace,
		OutputLimit:       outputLimit,
		Validate:          validate,
		ValidationSummary: validationSummary,
		AllowDangerous:    allowDangerous,
		DangerousReason:   overrideReason,
	}, cliRunActor)
}

func scopedRun(ctx context.Context, kernel runKernel, cwd, runID string) (runmodel.Show, error) {
	current, err := kernel.projects.Current(ctx, cwd)
	if err != nil {
		return runmodel.Show{}, err
	}

	shown, err := kernel.runs.ShowRun(ctx, runID)
	if err != nil {
		return runmodel.Show{}, err
	}
	if shown.Snapshot.ProjectID != current.ID {
		return runmodel.Show{}, runmodel.NewError(runmodel.CodeNotFound, "run was not found in the current project")
	}

	return shown, nil
}

func showContainsExecution(shown runmodel.Show, executionID string) bool {
	for _, execution := range shown.Executions {
		if execution.ID == executionID {
			return true
		}
	}

	return false
}

func showContainsArtifact(shown runmodel.Show, artifactID string) bool {
	for _, artifact := range shown.Artifacts {
		if artifact.ID == artifactID {
			return true
		}
	}

	return false
}

func runKernelCommand(ctx context.Context, database string, jsonOutput bool, output io.Writer, action func(runKernel) (any, error)) error {
	var kernel runKernel
	app := fx.New(
		fx.NopLogger,
		bootstrap.TaskOptions(database),
		fx.Invoke(func(projects *project.Service, tasks *taskapp.Service, runs *runapp.Service) {
			kernel = runKernel{projects: projects, tasks: tasks, runs: runs}
		}),
	)

	if err := app.Start(ctx); err != nil {
		if jsonOutput {
			return runJSONError(err)
		}

		return err
	}

	value, actionErr := action(kernel)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop run application: %w", stopErr)
	}
	combined := errors.Join(actionErr, stopErr)
	if combined != nil {
		if jsonOutput {
			return runJSONError(combined)
		}

		return combined
	}

	if jsonOutput {
		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{SchemaVersion: runSchemaVersion, Result: runOutputJSON(value)})
	}

	_, err := fmt.Fprintln(output, renderRunValue(value))
	return err
}

func runManagedCommand(ctx context.Context, database string, jsonOutput bool, output io.Writer, action func(runWorkflowKernel) (any, error)) error {
	var kernel runWorkflowKernel
	app := fx.New(
		fx.NopLogger,
		bootstrap.RunWorkflowOptions(database),
		fx.Invoke(func(projects *project.Service, tasks *taskapp.Service, runs *runapp.Service, workflow *runworkflow.Service) {
			kernel = runWorkflowKernel{
				runKernel: runKernel{projects: projects, tasks: tasks, runs: runs},
				workflow:  workflow,
			}
		}),
	)

	if err := app.Start(ctx); err != nil {
		if jsonOutput {
			return runJSONError(err)
		}

		return err
	}

	value, actionErr := action(kernel)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop managed run application: %w", stopErr)
	}
	combined := errors.Join(actionErr, stopErr)
	if combined != nil {
		if jsonOutput {
			return runJSONError(combined)
		}

		return combined
	}

	if jsonOutput {
		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{SchemaVersion: runSchemaVersion, Result: runOutputJSON(value)})
	}

	_, err := fmt.Fprintln(output, renderRunValue(value))
	return err
}

func runJSONError(err error) error {
	encoded, marshalErr := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{SchemaVersion: runSchemaVersion, Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: runErrorCode(err), Message: err.Error()}})
	if marshalErr != nil {
		return err
	}

	return jsonCommandError{value: string(encoded), cause: err}
}

func runErrorCode(err error) string {
	var runError *runmodel.Error
	if errors.As(err, &runError) {
		return string(runError.Code)
	}

	var taskError *task.Error
	if errors.As(err, &taskError) {
		return string(taskError.Code)
	}

	var projectError *project.Error
	if errors.As(err, &projectError) {
		return string(projectError.Code)
	}

	return string(runmodel.CodeInternal)
}

func renderRunValue(value any) string {
	switch typed := value.(type) {
	case runmodel.Run:
		return renderRun(typed)
	case []runListItem:
		return renderRunList(typed)
	case runmodel.Show:
		return renderRunShow(typed)
	case runShowView:
		return renderRunShowView(typed)
	case runmodel.Execution:
		return renderExecution(typed)
	case runmodel.Validation:
		return renderValidation(typed)
	case runmodel.FinishManagedExecutionResult:
		return renderManagedExecution(typed)
	case []runmodel.Artifact:
		return renderArtifacts(typed)
	case artifactVerificationResult:
		return renderArtifactVerification(typed)
	case taskCompletionResult:
		return renderCompletion(typed)
	default:
		return fmt.Sprint(value)
	}
}

func renderRun(value runmodel.Run) string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Run"))
	output.WriteString("\n\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s %s %s", presentation.StyledStatus(string(value.Status)), presentation.Neutral("·"), presentation.Metadata(fmt.Sprintf("revision %d", value.Revision)), presentation.Metadata(value.StartedAt.UTC().Format(time.RFC3339)))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("ID"), presentation.Warning(value.ID))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Task"), presentation.Warning(value.TaskID))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Snapshot"), presentation.Warning(value.ContextSnapshotID))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Lease"), presentation.Warning(value.LeaseID))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Expires"), presentation.Metadata(value.ExpiresAt.UTC().Format(time.RFC3339)))))
	if value.ResultSummary != "" {
		output.WriteString("\n\n")
		output.WriteString(presentation.RailLine(value.ResultSummary))
	}
	if value.ValidationOverride != "" {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Override"), presentation.Warning(value.ValidationOverride))))
	}

	return output.String()
}

func renderRunList(values []runListItem) string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Runs"))
	output.WriteString("\n\n")
	if len(values) == 0 {
		output.WriteString(presentation.Metadata("No runs."))
		return output.String()
	}

	rows := make([][]string, 0, len(values))
	for _, item := range values {
		value := item.Run
		rows = append(rows, []string{
			presentation.Warning(value.ID),
			presentation.StyledStatus(string(value.Status)),
			presentation.Warning(fmt.Sprintf("#%d", item.TaskNumber)),
			presentation.Metadata(value.StartedAt.UTC().Format("2006-01-02 15:04Z")),
		})
	}
	output.WriteString(presentation.RenderTable([]string{"ID", "STATE", "TASK", "STARTED"}, rows))

	return output.String()
}

func renderRunShow(value runmodel.Show) string {
	return renderRunShowView(runShowView{Show: value})
}

func renderRunShowView(value runShowView) string {
	var output strings.Builder
	output.WriteString(renderRun(value.Run))
	if value.TaskNumber > 0 {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Task number"), presentation.Warning(fmt.Sprintf("#%d", value.TaskNumber)))))
	}
	output.WriteString("\n\n")
	output.WriteString(presentation.SectionTitle("Context snapshot"))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Version"), presentation.Metadata(value.Snapshot.SchemaVersion))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Records"), presentation.Metadata(fmt.Sprint(len(value.Snapshot.Records))))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Retrieved"), presentation.Metadata(fmt.Sprint(len(value.Snapshot.Retrieval))))))
	if value.Snapshot.Metadata.WithoutRetrieval {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Retrieval override"), presentation.Warning(value.Snapshot.Metadata.OverrideReason))))
	}
	for _, record := range value.Snapshot.Records {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Metadata(fmt.Sprintf("r%d", record.RecordRevision)), record.Title)))
	}
	for _, item := range value.Snapshot.Retrieval {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s:%d-%d", presentation.Metadata(item.Kind), item.Path, item.LineStart, item.LineEnd)))
	}
	output.WriteString("\n\n")
	output.WriteString(presentation.SectionTitle("Evidence"))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%d executions · %d validations · %d artifacts", len(value.Executions), len(value.Validations), len(value.Artifacts))))
	for _, artifact := range value.Artifacts {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s %s", presentation.Metadata(string(artifact.Kind)), presentation.Warning(artifact.ID), presentation.Metadata(formatArtifactSize(artifact)))))
	}

	return output.String()
}

func renderExecution(value runmodel.Execution) string {
	return fmt.Sprintf(
		"%s\n\n%s\n%s\n%s",
		presentation.SectionTitle("Execution"),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.StyledStatus(string(value.Status)), presentation.Metadata(fmt.Sprintf("revision %d", value.Revision)))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("ID"), presentation.Warning(value.ID))),
		presentation.RailLine(strings.Join(value.Argv, " ")),
	)
}

func renderValidation(value runmodel.Validation) string {
	return fmt.Sprintf(
		"%s\n\n%s\n%s\n%s",
		presentation.SectionTitle("Validation"),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.StyledStatus(string(value.Status)), presentation.Metadata(string(value.Source)))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("ID"), presentation.Warning(value.ID))),
		presentation.RailLine(value.Summary),
	)
}

func renderManagedExecution(value runmodel.FinishManagedExecutionResult) string {
	var output strings.Builder
	output.WriteString(renderExecution(value.Execution))
	output.WriteString("\n\n")
	output.WriteString(presentation.SectionTitle("Artifacts"))
	for _, artifact := range value.Artifacts {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s %s", presentation.Metadata(string(artifact.Kind)), presentation.Warning(artifact.ID), presentation.Metadata(formatArtifactSize(artifact)))))
	}
	if value.Validation != nil {
		output.WriteString("\n\n")
		output.WriteString(renderValidation(*value.Validation))
	}

	return output.String()
}

func renderArtifacts(values []runmodel.Artifact) string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Artifacts"))
	output.WriteString("\n\n")
	if len(values) == 0 {
		output.WriteString(presentation.Metadata("No artifacts."))
		return output.String()
	}

	rows := make([][]string, 0, len(values))
	for _, artifact := range values {
		rows = append(rows, []string{
			presentation.Metadata(string(artifact.Kind)),
			presentation.Warning(artifact.ID),
			presentation.Metadata(formatArtifactSize(artifact)),
			presentation.Metadata(artifact.SHA256[:12]),
		})
	}
	output.WriteString(presentation.RenderTable([]string{"KIND", "ID", "SIZE", "SHA-256"}, rows))

	return output.String()
}

func renderArtifactVerification(value artifactVerificationResult) string {
	return fmt.Sprintf(
		"%s\n\n%s\n%s\n%s",
		presentation.SectionTitle("Artifact verified"),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("ID"), presentation.Warning(value.Artifact.ID))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("SHA-256"), presentation.Metadata(value.Artifact.SHA256))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Size"), presentation.Metadata(formatArtifactSize(value.Artifact)))),
	)
}

func formatArtifactSize(value runmodel.Artifact) string {
	result := fmt.Sprintf("%d B", value.StoredSize)
	if value.Truncated {
		result = fmt.Sprintf("%d/%d B truncated", value.StoredSize, value.OriginalSize)
	}

	return result
}

func renderCompletion(value taskCompletionResult) string {
	return fmt.Sprintf(
		"%s\n\n%s\n%s\n%s",
		presentation.SectionTitle("Task completed"),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.StyledStatus(string(value.Task.Status)), presentation.Metadata(fmt.Sprintf("revision %d", value.Task.Revision)))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Completion"), presentation.Warning(value.Completion.ID))),
		presentation.RailLine(value.Completion.Note),
	)
}

func gitValue(ctx context.Context, cwd string, args ...string) string {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", filepath.Clean(cwd)}, args...)...)
	value, err := command.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(value))
}

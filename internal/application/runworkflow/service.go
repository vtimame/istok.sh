// Package runworkflow implements the local managed process workflow around the
// transport-independent Run application service.
package runworkflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	runapp "github.com/vtimame/istok.sh/internal/application/run"
	"github.com/vtimame/istok.sh/internal/artifactstore"
	"github.com/vtimame/istok.sh/internal/commandpolicy"
	"github.com/vtimame/istok.sh/internal/executor"
	"github.com/vtimame/istok.sh/internal/project"
	runmodel "github.com/vtimame/istok.sh/internal/run"
)

const defaultLeaseDuration = 15 * time.Minute

type ExecuteInput struct {
	RunID             string
	LeaseID           string
	ProjectRoot       string
	CWD               string
	Argv              []string
	Environment       []string
	Timeout           time.Duration
	TerminationGrace  time.Duration
	OutputLimit       int64
	Validate          bool
	ValidationSummary string
	AllowDangerous    bool
	DangerousReason   string
}

func (input ExecuteInput) validate() error {
	if !runmodel.IsUUIDv7(input.RunID) || !runmodel.IsUUIDv7(input.LeaseID) {
		return runmodel.NewError(runmodel.CodeInvalid, "run id and lease id must be canonical UUIDv7 values")
	}
	if len(input.Argv) == 0 || strings.TrimSpace(input.Argv[0]) == "" {
		return runmodel.NewError(runmodel.CodeInvalid, "command argv is required")
	}
	if input.Timeout <= 0 || input.TerminationGrace <= 0 {
		return runmodel.NewError(runmodel.CodeInvalid, "positive timeout and termination grace are required")
	}
	if input.Timeout > time.Duration(1<<63-1)-input.TerminationGrace-time.Minute {
		return runmodel.NewError(runmodel.CodeInvalid, "timeout and termination grace are too large")
	}
	if input.OutputLimit < 0 {
		return runmodel.NewError(runmodel.CodeInvalid, "output limit must be non-negative")
	}

	return nil
}

type Service struct {
	runs      *runapp.Service
	artifacts *artifactstore.Store
	execute   func(context.Context, executor.Request) (executor.Result, error)
}

func NewService(runs *runapp.Service, artifacts *artifactstore.Store) *Service {
	return &Service{runs: runs, artifacts: artifacts, execute: executor.Execute}
}

func (service *Service) Execute(ctx context.Context, input ExecuteInput, actor runmodel.ActorSnapshot) (runmodel.FinishManagedExecutionResult, error) {
	if err := input.validate(); err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	if err := actor.Validate(); err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}

	dangerousOverride, err := authorizeCommand(input)
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	cwd, err := scopedWorkingDirectory(input.ProjectRoot, input.CWD)
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}

	executionID, err := runmodel.NewID()
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	stdout, err := service.artifacts.NewCapture(input.RunID, executionID, string(runmodel.ArtifactKindStdout), input.OutputLimit)
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}
	defer stdout.Abort()

	stderr, err := service.artifacts.NewCapture(input.RunID, executionID, string(runmodel.ArtifactKindStderr), input.OutputLimit)
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, errors.Join(err, stdout.Abort())
	}
	defer stderr.Abort()

	leaseDuration := input.Timeout + input.TerminationGrace + time.Minute
	if leaseDuration < defaultLeaseDuration {
		leaseDuration = defaultLeaseDuration
	}
	if _, err := service.runs.Heartbeat(ctx, runmodel.HeartbeatInput{
		RunID:         input.RunID,
		LeaseID:       input.LeaseID,
		LeaseDuration: leaseDuration,
	}, actor); err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}

	redactedArgv := commandpolicy.Redact(input.Argv)
	execution, err := service.runs.StartExecution(ctx, runmodel.StartExecutionInput{
		ID:                executionID,
		RunID:             input.RunID,
		LeaseID:           input.LeaseID,
		Argv:              redactedArgv,
		CWD:               cwd,
		DangerousOverride: dangerousOverride,
	}, actor)
	if err != nil {
		return runmodel.FinishManagedExecutionResult{}, err
	}

	processStarted := time.Now()
	processResult, processErr := service.execute(ctx, executor.Request{
		Argv:             append([]string(nil), input.Argv...),
		Dir:              cwd,
		Env:              executionEnvironment(input.Environment, input, executionID, cwd),
		Stdout:           stdout,
		Stderr:           stderr,
		Timeout:          input.Timeout,
		TerminationGrace: input.TerminationGrace,
	})
	if processErr != nil {
		_, _ = fmt.Fprintln(stderr, processErr)
		input.Validate = false
	}

	finish := processFinish(execution, processResult, processStarted, processErr)
	finishCtx, cancelFinish := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFinish()

	files, err := commitArtifacts(stdout, stderr)
	if err != nil {
		finish.Status = runmodel.ExecutionFailed
		finish.Signal = "artifact_error"
		_, finishErr := service.runs.FinishExecution(finishCtx, finish, actor)
		var cleanupErr error
		for _, file := range files {
			cleanupErr = errors.Join(cleanupErr, service.artifacts.Remove(file))
		}

		return runmodel.FinishManagedExecutionResult{}, errors.Join(err, cleanupErr, finishErr)
	}

	managed, err := service.managedFinish(finishCtx, input, redactedArgv, finish, files, actor)
	if err != nil {
		cleanupErr := errors.Join(service.artifacts.Remove(files[0]), service.artifacts.Remove(files[1]))
		finish.Status = runmodel.ExecutionFailed
		finish.Signal = "persistence_error"
		_, fallbackErr := service.runs.FinishExecution(finishCtx, finish, actor)

		return runmodel.FinishManagedExecutionResult{}, errors.Join(err, cleanupErr, fallbackErr)
	}
	if processErr != nil {
		return managed, processErr
	}

	return managed, nil
}

func (service *Service) VerifyArtifact(ctx context.Context, artifactID string) (runmodel.Artifact, error) {
	artifact, err := service.runs.GetArtifact(ctx, artifactID)
	if err != nil {
		return runmodel.Artifact{}, err
	}

	err = service.artifacts.Verify(artifactstore.File{
		RelativePath: artifact.RelativePath,
		SHA256:       artifact.SHA256,
		OriginalSize: artifact.OriginalSize,
		StoredSize:   artifact.StoredSize,
		Truncated:    artifact.Truncated,
		MediaType:    artifact.MediaType,
	})
	if err != nil {
		return runmodel.Artifact{}, fmt.Errorf("verify managed artifact: %w", err)
	}

	return artifact, nil
}

func (service *Service) managedFinish(
	ctx context.Context,
	input ExecuteInput,
	redactedArgv []string,
	finish runmodel.FinishExecutionInput,
	files []artifactstore.File,
	actor runmodel.ActorSnapshot,
) (runmodel.FinishManagedExecutionResult, error) {
	artifacts := make([]runmodel.ArtifactInput, 0, 2)
	for index, kind := range []runmodel.ArtifactKind{runmodel.ArtifactKindStdout, runmodel.ArtifactKindStderr} {
		id, err := runmodel.NewID()
		if err != nil {
			return runmodel.FinishManagedExecutionResult{}, err
		}

		artifacts = append(artifacts, artifactInput(id, finish.ExecutionID, kind, files[index]))
	}

	managed := runmodel.FinishManagedExecutionInput{
		FinishExecutionInput: finish,
		Artifacts:            artifacts,
	}
	if input.Validate {
		validationID, err := runmodel.NewID()
		if err != nil {
			return runmodel.FinishManagedExecutionResult{}, err
		}

		status := runmodel.ValidationStatusFailed
		if finish.Status == runmodel.ExecutionSucceeded && finish.ExitCode != nil && *finish.ExitCode == 0 {
			status = runmodel.ValidationStatusPassed
		}
		summary := strings.TrimSpace(input.ValidationSummary)
		if summary == "" {
			summary = "validation " + string(status)
		}
		managed.Validation = &runmodel.RecordValidationInput{
			ID:          validationID,
			ExecutionID: finish.ExecutionID,
			Source:      runmodel.ValidationSourceExecuted,
			Status:      status,
			Command:     formatCommand(redactedArgv),
			ExitCode:    finish.ExitCode,
			DurationMS:  finish.DurationMS,
			Summary:     summary,
		}
	}

	return service.runs.FinishManagedExecution(ctx, managed, actor)
}

func authorizeCommand(input ExecuteInput) (string, error) {
	decision := commandpolicy.Inspect(input.Argv)
	if !decision.Dangerous {
		return "", nil
	}
	if !input.AllowDangerous {
		return "", runmodel.NewError(runmodel.CodeInvalid, "dangerous command requires --allow-dangerous: %s", decision.Reason)
	}

	reason := strings.TrimSpace(input.DangerousReason)
	if reason == "" {
		return "", runmodel.NewError(runmodel.CodeInvalid, "--override-reason is required with --allow-dangerous")
	}

	return reason, nil
}

func scopedWorkingDirectory(root, requested string) (string, error) {
	projectRoot, err := project.Canonicalize(root)
	if err != nil {
		return "", err
	}
	candidate, err := project.Canonicalize(requested)
	if err != nil {
		return "", err
	}

	relative, err := filepath.Rel(projectRoot.CanonicalPath, candidate.CanonicalPath)
	if err != nil {
		return "", fmt.Errorf("compare execution working directory with project root: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", runmodel.NewError(runmodel.CodeInvalid, "execution working directory must be inside the current project")
	}

	return candidate.CanonicalPath, nil
}

func executionEnvironment(base []string, input ExecuteInput, executionID, cwd string) []string {
	return executor.FilterEnvironment(base, map[string]string{
		"ISTOK_EXECUTION_ID": executionID,
		"ISTOK_PROJECT_ROOT": input.ProjectRoot,
		"ISTOK_RUN_ID":       input.RunID,
		"PWD":                cwd,
	})
}

func processFinish(execution runmodel.Execution, result executor.Result, started time.Time, processErr error) runmodel.FinishExecutionInput {
	duration := time.Since(started).Milliseconds()
	finish := runmodel.FinishExecutionInput{
		ExecutionID:      execution.ID,
		ExpectedRevision: execution.Revision,
		Status:           runmodel.ExecutionFailed,
		DurationMS:       &duration,
		Signal:           result.Signal,
		TimedOut:         result.TimedOut,
	}
	if processErr == nil {
		finish.ExitCode = &result.ExitCode
	}
	if result.Cancelled {
		finish.Status = runmodel.ExecutionCancelled
	} else if processErr == nil && !result.TimedOut && result.Signal == "" && result.ExitCode == 0 {
		finish.Status = runmodel.ExecutionSucceeded
	}

	return finish
}

func commitArtifacts(stdout, stderr *artifactstore.Capture) ([]artifactstore.File, error) {
	stdoutFile, err := stdout.Commit()
	if err != nil {
		return nil, err
	}
	stderrFile, err := stderr.Commit()
	if err != nil {
		return []artifactstore.File{stdoutFile}, err
	}

	return []artifactstore.File{stdoutFile, stderrFile}, nil
}

func artifactInput(id, executionID string, kind runmodel.ArtifactKind, file artifactstore.File) runmodel.ArtifactInput {
	return runmodel.ArtifactInput{
		ID:           id,
		ExecutionID:  executionID,
		Kind:         kind,
		RelativePath: file.RelativePath,
		SHA256:       file.SHA256,
		OriginalSize: file.OriginalSize,
		StoredSize:   file.StoredSize,
		Truncated:    file.Truncated,
		MediaType:    file.MediaType,
	}
}

func formatCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for index, arg := range argv {
		if arg != "" && !strings.ContainsAny(arg, " \t\n\r\"'") {
			quoted[index] = arg
			continue
		}

		quoted[index] = strconv.Quote(arg)
	}

	return strings.Join(quoted, " ")
}

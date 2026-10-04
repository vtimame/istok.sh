package runapp

import (
	"context"
	"strings"
	"time"

	contextpack "github.com/vtimame/istok.sh/internal/contextpack"
	run "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type Service struct {
	repository     Repository
	taskResolver   TaskResolver
	contextBuilder ContextPackageBuilder
}

const defaultLeaseDuration = 15 * time.Minute

func NewService(repository Repository, taskResolver TaskResolver, contextBuilder ContextPackageBuilder) *Service {
	return &Service{
		repository:     repository,
		taskResolver:   taskResolver,
		contextBuilder: contextBuilder,
	}
}

func (s *Service) Claim(ctx context.Context, selector task.Selector, input run.ClaimInput, actor run.ActorSnapshot) (run.Run, error) {
	if err := selector.Validate(); err != nil {
		return run.Run{}, err
	}
	if err := input.Validate(); err != nil {
		return run.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Run{}, err
	}

	taskValue, err := s.taskResolver.Resolve(ctx, selector, false)
	if err != nil {
		return run.Run{}, err
	}
	if taskValue.Status != task.StatusOpen {
		return run.Run{}, run.NewError(run.CodeInvalidTransition, "only open tasks can be claimed")
	}
	if err := taskValue.RequireActive(); err != nil {
		return run.Run{}, err
	}

	pkg, err := s.contextBuilder.BuildForTask(ctx, taskValue, contextpack.BuildOptions{ContextLimit: input.ContextLimit, ExplicitContextIDs: input.ContextIDs, LegacyAllContext: input.AllContext, ContextOverrideReason: input.ContextOverrideReason, WithoutRetrieval: input.WithoutRetrieval, RetrievalOverrideReason: input.RetrievalOverrideReason})
	if err != nil {
		return run.Run{}, err
	}

	runID := input.ID
	if runID == "" {
		runID, err = run.NewID()
		if err != nil {
			return run.Run{}, err
		}
	}

	snapshotID := input.SnapshotID
	if snapshotID == "" {
		snapshotID, err = run.NewID()
		if err != nil {
			return run.Run{}, err
		}
	}

	abandonEventID, err := run.NewID()
	if err != nil {
		return run.Run{}, err
	}
	eventID, err := run.NewID()
	if err != nil {
		return run.Run{}, err
	}

	snapshot, err := run.NewContextSnapshot(snapshotID, pkg, taskValue.ProjectID)
	if err != nil {
		return run.Run{}, err
	}

	leaseID, err := run.NewID()
	if err != nil {
		return run.Run{}, err
	}

	record := run.ClaimRecord{
		RunID:          runID,
		TaskID:         taskValue.ID,
		EventID:        eventID,
		AbandonEventID: abandonEventID,
		Snapshot:       snapshot,
		BaseBranch:     input.BaseBranch,
		BaseCommit:     input.BaseCommit,
		Actor:          actor,
		LeaseID:        leaseID,
		LeaseDuration:  defaultLeaseDuration,
	}

	return s.repository.Claim(ctx, record)
}

func (s *Service) PreviewContext(ctx context.Context, selector task.Selector, input run.ClaimInput) (contextpack.Package, error) {
	if err := selector.Validate(); err != nil {
		return contextpack.Package{}, err
	}
	if err := input.Validate(); err != nil {
		return contextpack.Package{}, err
	}

	taskValue, err := s.taskResolver.Resolve(ctx, selector, false)
	if err != nil {
		return contextpack.Package{}, err
	}

	return s.contextBuilder.BuildForTask(ctx, taskValue, contextpack.BuildOptions{
		ContextLimit:            input.ContextLimit,
		ExplicitContextIDs:      input.ContextIDs,
		LegacyAllContext:        input.AllContext,
		ContextOverrideReason:   input.ContextOverrideReason,
		WithoutRetrieval:        input.WithoutRetrieval,
		RetrievalOverrideReason: input.RetrievalOverrideReason,
	})
}

func (s *Service) GetRun(ctx context.Context, runID string) (run.Run, error) {
	if !run.IsUUIDv7(runID) {
		return run.Run{}, run.NewError(run.CodeInvalid, "run id must be a canonical UUIDv7")
	}

	return s.repository.GetRun(ctx, runID)
}

func (s *Service) ListRuns(ctx context.Context, options run.ListOptions) ([]run.Run, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	return s.repository.ListRuns(ctx, options)
}

func (s *Service) ShowRun(ctx context.Context, runID string) (run.Show, error) {
	if !run.IsUUIDv7(runID) {
		return run.Show{}, run.NewError(run.CodeInvalid, "run id must be a canonical UUIDv7")
	}

	return s.repository.ShowRun(ctx, runID)
}

func (s *Service) HasActiveRun(ctx context.Context, taskID string) (bool, error) {
	if !run.IsUUIDv7(taskID) {
		return false, run.NewError(run.CodeInvalid, "task id must be a canonical UUIDv7")
	}

	return s.repository.HasActiveRun(ctx, taskID)
}

func (s *Service) TaskRunState(ctx context.Context, taskID string) (run.TaskRunState, error) {
	if !run.IsUUIDv7(taskID) {
		return run.TaskRunState{}, run.NewError(run.CodeInvalid, "task id must be a canonical UUIDv7")
	}

	return s.repository.TaskRunState(ctx, taskID)
}

func (s *Service) StartExecution(ctx context.Context, input run.StartExecutionInput, actor run.ActorSnapshot) (run.Execution, error) {
	if err := input.Validate(); err != nil {
		return run.Execution{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Execution{}, err
	}

	id := input.ID
	if id == "" {
		var err error
		id, err = run.NewID()
		if err != nil {
			return run.Execution{}, err
		}
		input.ID = id
	}
	input.DangerousOverride = trim(input.DangerousOverride)

	return s.repository.StartExecution(ctx, input, actor)
}

func (s *Service) FinishExecution(ctx context.Context, input run.FinishExecutionInput, actor run.ActorSnapshot) (run.Execution, error) {
	if err := input.Validate(); err != nil {
		return run.Execution{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Execution{}, err
	}

	return s.repository.FinishExecution(ctx, input, actor)
}

func (s *Service) FinishManagedExecution(ctx context.Context, input run.FinishManagedExecutionInput, actor run.ActorSnapshot) (run.FinishManagedExecutionResult, error) {
	for index := range input.Artifacts {
		if input.Artifacts[index].ID == "" {
			id, err := run.NewID()
			if err != nil {
				return run.FinishManagedExecutionResult{}, err
			}
			input.Artifacts[index].ID = id
		}
	}
	if err := input.Validate(); err != nil {
		return run.FinishManagedExecutionResult{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.FinishManagedExecutionResult{}, err
	}
	if input.Validation != nil && input.Validation.Source != run.ValidationSourceExecuted {
		return run.FinishManagedExecutionResult{}, run.NewError(run.CodeInvalid, "managed validation source must be executed")
	}
	if input.Validation != nil && input.Validation.ID == "" {
		id, err := run.NewID()
		if err != nil {
			return run.FinishManagedExecutionResult{}, err
		}
		input.Validation.ID = id
	}
	return s.repository.FinishManagedExecution(ctx, input, actor)
}

func (s *Service) Heartbeat(ctx context.Context, input run.HeartbeatInput, actor run.ActorSnapshot) (run.Run, error) {
	if input.LeaseDuration <= 0 {
		input.LeaseDuration = defaultLeaseDuration
	}
	if err := input.Validate(); err != nil {
		return run.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Run{}, err
	}
	return s.repository.Heartbeat(ctx, input, actor)
}

func (s *Service) Recover(ctx context.Context, input run.RecoverInput, actor run.ActorSnapshot) (run.Run, error) {
	if input.LeaseID == "" {
		leaseID, err := run.NewID()
		if err != nil {
			return run.Run{}, err
		}
		input.LeaseID = leaseID
	}
	if input.LeaseDuration <= 0 {
		input.LeaseDuration = defaultLeaseDuration
	}
	if input.EventID == "" {
		eventID, err := run.NewID()
		if err != nil {
			return run.Run{}, err
		}
		input.EventID = eventID
	}
	if err := input.Validate(); err != nil {
		return run.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Run{}, err
	}
	input.Reason = trim(input.Reason)
	return s.repository.Recover(ctx, input, actor)
}

func (s *Service) Abandon(ctx context.Context, input run.AbandonInput, actor run.ActorSnapshot) (run.Run, error) {
	if input.EventID == "" {
		eventID, err := run.NewID()
		if err != nil {
			return run.Run{}, err
		}
		input.EventID = eventID
	}
	if err := input.Validate(); err != nil {
		return run.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Run{}, err
	}
	input.Reason = trim(input.Reason)

	return s.repository.Abandon(ctx, input, actor)
}

func (s *Service) GetArtifact(ctx context.Context, id string) (run.Artifact, error) {
	if !run.IsUUIDv7(id) {
		return run.Artifact{}, run.NewError(run.CodeInvalid, "artifact id must be a canonical UUIDv7")
	}
	return s.repository.GetArtifact(ctx, id)
}

func (s *Service) ListArtifacts(ctx context.Context, executionID string) ([]run.Artifact, error) {
	if !run.IsUUIDv7(executionID) {
		return nil, run.NewError(run.CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	return s.repository.ListArtifacts(ctx, executionID)
}

func (s *Service) RecordValidation(ctx context.Context, input run.RecordValidationInput, actor run.ActorSnapshot) (run.Validation, error) {
	if err := input.Validate(); err != nil {
		return run.Validation{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Validation{}, err
	}
	if input.Source != run.ValidationSourceAttested {
		return run.Validation{}, run.NewError(run.CodeInvalid, "manual validations must be attested")
	}

	id := input.ID
	if id == "" {
		var err error
		id, err = run.NewID()
		if err != nil {
			return run.Validation{}, err
		}
		input.ID = id
	}

	input.Command = trim(input.Command)
	input.Summary = trim(input.Summary)

	return s.repository.RecordValidation(ctx, input, actor)
}

func (s *Service) FinishRun(ctx context.Context, input run.FinishRunInput, actor run.ActorSnapshot) (run.Run, error) {
	if err := input.Validate(); err != nil {
		return run.Run{}, err
	}
	if err := actor.Validate(); err != nil {
		return run.Run{}, err
	}
	if input.EventID == "" {
		eventID, err := run.NewID()
		if err != nil {
			return run.Run{}, err
		}

		input.EventID = eventID
	}

	input.ResultSummary = trim(input.ResultSummary)
	if !input.AllowUnvalidated {
		input.OverrideReason = ""
	}

	return s.repository.FinishRun(ctx, input, actor)
}

func (s *Service) CompleteTask(ctx context.Context, selector task.Selector, input run.CompleteTaskInput, actor run.ActorSnapshot) (task.Task, run.Completion, error) {
	if err := selector.Validate(); err != nil {
		return task.Task{}, run.Completion{}, err
	}
	if err := input.Validate(); err != nil {
		return task.Task{}, run.Completion{}, err
	}
	if err := actor.Validate(); err != nil {
		return task.Task{}, run.Completion{}, err
	}

	taskValue, err := s.taskResolver.Resolve(ctx, selector, false)
	if err != nil {
		return task.Task{}, run.Completion{}, err
	}
	if err := taskValue.RequireActive(); err != nil {
		return task.Task{}, run.Completion{}, err
	}

	completionID := input.ID
	if completionID == "" {
		completionID, err = run.NewID()
		if err != nil {
			return task.Task{}, run.Completion{}, err
		}
	}

	eventID, err := run.NewID()
	if err != nil {
		return task.Task{}, run.Completion{}, err
	}

	record := run.CompletionRecord{
		ID:                   completionID,
		TaskID:               taskValue.ID,
		EventID:              eventID,
		ExpectedTaskRevision: input.ExpectedTaskRevision,
		Note:                 trim(input.Note),
		OverrideReason:       trim(input.OverrideReason),
		Actor:                actor,
		CreatedAt:            time.Now(),
	}
	runID := trim(input.RunID)
	validationID := trim(input.ValidationID)
	if input.RunID != "" {
		record.RunID = &runID
	}
	if input.ValidationID != "" {
		record.ValidationID = &validationID
	}

	return s.repository.CompleteTask(ctx, record)
}

func trim(value string) string {
	return strings.TrimSpace(value)
}

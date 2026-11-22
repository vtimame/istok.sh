package runapp

import (
	"context"
	"errors"

	run "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

// The calls in this file work on "my active run of this task", so an agent
// does not have to carry run IDs, lease IDs and revisions between calls.

// TaskEvidenceInput is a check made outside Istok: a command, a CI pipeline
// or a manual end-to-end test.
type TaskEvidenceInput struct {
	Status     run.ValidationStatus
	Summary    string
	Command    string
	ExitCode   *int
	DurationMS *int64
	OutputTail string

	// CWD is the directory the evidence is recorded in, the project root.
	CWD string
}

// RecordTaskEvidence records the check on the actor's active run of the task.
func (s *Service) RecordTaskEvidence(ctx context.Context, selector task.Selector, input TaskEvidenceInput, actor run.ActorSnapshot) (run.Evidence, error) {
	if err := actor.Validate(); err != nil {
		return run.Evidence{}, err
	}

	_, active, err := s.ownedActiveRun(ctx, selector, actor)
	if err != nil {
		return run.Evidence{}, err
	}

	s.touch(ctx, active.ID, active.LeaseID, actor)

	executionID, err := run.NewID()
	if err != nil {
		return run.Evidence{}, err
	}
	validationID, err := run.NewID()
	if err != nil {
		return run.Evidence{}, err
	}

	return s.repository.RecordEvidence(ctx, run.RecordEvidenceInput{
		ExecutionID:  executionID,
		ValidationID: validationID,
		RunID:        active.ID,
		LeaseID:      active.LeaseID,
		CWD:          input.CWD,
		Status:       input.Status,
		Summary:      input.Summary,
		Command:      input.Command,
		ExitCode:     input.ExitCode,
		DurationMS:   input.DurationMS,
		OutputTail:   input.OutputTail,
	}, actor)
}

// TaskDoneInput closes a task. Without NotVerifiedReason the run needs a
// passed validation: ValidationID, or the latest passed one of the run.
type TaskDoneInput struct {
	Summary           string
	ValidationID      string
	NotVerifiedReason string
}

// TaskDone is the result of closing a task.
type TaskDone struct {
	Task       task.Task      `json:"task"`
	Run        run.Run        `json:"run"`
	Completion run.Completion `json:"completion"`
}

// DoneTask finishes the actor's active run of the task and completes the
// task. A run the actor already finished as succeeded is reused, so a retry
// after a failed completion does not need a new run.
func (s *Service) DoneTask(ctx context.Context, selector task.Selector, input TaskDoneInput, actor run.ActorSnapshot) (TaskDone, error) {
	if err := actor.Validate(); err != nil {
		return TaskDone{}, err
	}

	summary := trim(input.Summary)
	reason := trim(input.NotVerifiedReason)
	if summary == "" {
		return TaskDone{}, run.NewError(run.CodeInvalid, "summary is required")
	}

	taskValue, finished, err := s.finishedOrActiveRun(ctx, selector, actor)
	if err != nil {
		return TaskDone{}, err
	}

	validationID := trim(input.ValidationID)
	if validationID == "" && reason == "" {
		validationID, err = s.latestPassedValidation(ctx, finished.ID)
		if err != nil {
			return TaskDone{}, err
		}
	}
	if validationID != "" {
		reason = ""
	}

	if finished.Status == run.StatusActive {
		finished, err = s.FinishRun(ctx, run.FinishRunInput{
			RunID:            finished.ID,
			LeaseID:          finished.LeaseID,
			ExpectedRevision: finished.Revision,
			Status:           run.StatusSucceeded,
			ResultSummary:    summary,
			AllowUnvalidated: reason != "",
			OverrideReason:   reason,
		}, actor)
		if err != nil {
			return TaskDone{}, err
		}

		taskValue, err = s.taskResolver.Resolve(ctx, selector, false)
		if err != nil {
			return TaskDone{}, err
		}
	}

	completed, completion, err := s.CompleteTask(ctx, selector, run.CompleteTaskInput{
		ExpectedTaskRevision: taskValue.Revision,
		RunID:                finished.ID,
		ValidationID:         validationID,
		Note:                 summary,
		OverrideReason:       reason,
	}, actor)
	if err != nil {
		return TaskDone{}, err
	}

	return TaskDone{Task: completed, Run: finished, Completion: completion}, nil
}

// ReleaseTask ends the actor's active run of the task as cancelled and leaves
// the task open, so another agent can start it at once instead of waiting for
// the lease to expire. When the actor has no active run of the task, because
// none is active or another agent's is, there is nothing to release: the call
// succeeds and the released run is nil.
func (s *Service) ReleaseTask(ctx context.Context, selector task.Selector, summary string, actor run.ActorSnapshot) (*run.Run, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}

	taskValue, active, err := s.ownedActiveRun(ctx, selector, actor)
	var runErr *run.Error
	if errors.As(err, &runErr) && (runErr.Code == run.CodeInvalidTransition || runErr.Code == run.CodeConflict) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	summary = trim(summary)
	if summary == "" {
		summary = "handed over to another agent"
	}

	released, err := s.FinishRun(ctx, run.FinishRunInput{
		RunID:            active.ID,
		LeaseID:          active.LeaseID,
		ExpectedRevision: active.Revision,
		Status:           run.StatusCancelled,
		ResultSummary:    summary,
	}, actor)
	if err != nil {
		return nil, err
	}

	// Free the task on the hub now, so an agent on another device does not
	// wait for the next sync.
	if s.leases != nil {
		s.leases.ReleaseLease(ctx, taskValue.ProjectID, taskValue.ID, active.ID)
	}

	return &released, nil
}

// StartTask claims the task or, when the actor already holds its active run,
// continues that run: its lease is extended or revived and Resumed is true.
// Calling it again after a restart or a context compaction is safe.
func (s *Service) StartTask(ctx context.Context, selector task.Selector, input run.ClaimInput, actor run.ActorSnapshot) (run.Run, bool, error) {
	if err := actor.Validate(); err != nil {
		return run.Run{}, false, err
	}

	taskValue, active, err := s.ownedActiveRun(ctx, selector, actor)
	if err == nil {
		// The run may have been claimed on another device by the same agent:
		// continue it here only when that device no longer works on it.
		if err := s.acquireLease(ctx, taskValue.ProjectID, taskValue.ID, active.ID); err != nil {
			return run.Run{}, false, err
		}
		s.touch(ctx, active.ID, active.LeaseID, actor)

		current, err := s.repository.GetRun(ctx, active.ID)
		if err != nil {
			return run.Run{}, false, err
		}

		return current, true, nil
	}

	var runErr *run.Error
	if !errors.As(err, &runErr) || runErr.Code != run.CodeInvalidTransition {
		return run.Run{}, false, err
	}

	claimed, err := s.Claim(ctx, selector, input, actor)
	if err != nil {
		return run.Run{}, false, err
	}

	return claimed, false, nil
}

// OwnedActiveRun returns the actor's active run of the task, with its lease,
// so a call can name the task instead of the run and its lease.
func (s *Service) OwnedActiveRun(ctx context.Context, selector task.Selector, actor run.ActorSnapshot) (run.Run, error) {
	if err := actor.Validate(); err != nil {
		return run.Run{}, err
	}

	_, active, err := s.ownedActiveRun(ctx, selector, actor)
	return active, err
}

// ownedActiveRun returns the task and its active run when the actor owns the
// run's lease.
func (s *Service) ownedActiveRun(ctx context.Context, selector task.Selector, actor run.ActorSnapshot) (task.Task, run.Run, error) {
	taskValue, err := s.taskResolver.Resolve(ctx, selector, false)
	if err != nil {
		return task.Task{}, run.Run{}, err
	}

	runs, err := s.repository.ListRuns(ctx, run.ListOptions{TaskID: taskValue.ID, Statuses: []run.Status{run.StatusActive}, Limit: 1})
	if err != nil {
		return task.Task{}, run.Run{}, err
	}
	if len(runs) == 0 {
		return task.Task{}, run.Run{}, run.NewError(run.CodeInvalidTransition, "the task has no active run; claim it with task_claim first")
	}
	if runs[0].LeaseOwner.ID != actor.ID {
		return task.Task{}, run.Run{}, run.NewError(run.CodeConflict, "the task's active run belongs to another agent")
	}

	return taskValue, runs[0], nil
}

// finishedOrActiveRun returns the actor's active run of the task or, when
// there is none, the actor's latest succeeded run.
func (s *Service) finishedOrActiveRun(ctx context.Context, selector task.Selector, actor run.ActorSnapshot) (task.Task, run.Run, error) {
	taskValue, active, err := s.ownedActiveRun(ctx, selector, actor)
	if err == nil {
		return taskValue, active, nil
	}

	var runErr *run.Error
	if !errors.As(err, &runErr) || runErr.Code != run.CodeInvalidTransition {
		return task.Task{}, run.Run{}, err
	}

	taskValue, resolveErr := s.taskResolver.Resolve(ctx, selector, false)
	if resolveErr != nil {
		return task.Task{}, run.Run{}, resolveErr
	}

	succeeded, listErr := s.repository.ListRuns(ctx, run.ListOptions{TaskID: taskValue.ID, Statuses: []run.Status{run.StatusSucceeded}, Limit: 20})
	if listErr != nil {
		return task.Task{}, run.Run{}, listErr
	}
	for _, value := range succeeded {
		if value.Actor.ID == actor.ID {
			return taskValue, value, nil
		}
	}

	return task.Task{}, run.Run{}, err
}

func (s *Service) latestPassedValidation(ctx context.Context, runID string) (string, error) {
	show, err := s.repository.ShowRun(ctx, runID)
	if err != nil {
		return "", err
	}

	for i := len(show.Validations) - 1; i >= 0; i-- {
		if show.Validations[i].Status == run.ValidationStatusPassed {
			return show.Validations[i].ID, nil
		}
	}

	return "", run.NewError(run.CodeEvidenceRequired, "the run has no passed validation: run the check with run_validate (or record one Istok cannot run with task_evidence), or pass not_verified_reason")
}

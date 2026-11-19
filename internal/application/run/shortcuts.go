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

	return "", run.NewError(run.CodeEvidenceRequired, "the run has no passed validation: record one with task_evidence or run_validate, or pass not_verified_reason")
}

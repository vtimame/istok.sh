// Package handoffapp assembles where a task stands for the next session: the
// latest handoff note, recent notes and the outcome of earlier runs. An agent
// reads it after a restart or a context compaction instead of reloading the
// whole task history.
package handoffapp

import (
	"context"
	"time"

	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

const (
	// noteLimit bounds the recent notes in a brief.
	noteLimit = 5

	// runLimit bounds the earlier runs in a brief.
	runLimit = 3

	// resumeWindow is how long after its lease expired an active run still
	// counts as interrupted work worth resuming.
	resumeWindow = 24 * time.Hour

	// resumeRunLimit bounds the active runs a resume reads.
	resumeRunLimit = 50
)

// TaskReader reads a task with its events.
type TaskReader interface {
	Show(context.Context, task.Selector, bool) (task.Show, error)
}

// RunReader lists runs.
type RunReader interface {
	ListRuns(context.Context, run.ListOptions) ([]run.Run, error)
}

// Brief is where one task stands.
type Brief struct {
	Task task.Task `json:"task"`

	// Handoff is the latest handoff note, if any.
	Handoff *task.Event `json:"handoff,omitempty"`

	// Notes are the latest progress, blocker and comment events after the
	// handoff, oldest first.
	Notes []task.Event `json:"notes"`

	// ActiveRun is the task's active run, if any.
	ActiveRun *RunBrief `json:"active_run,omitempty"`

	// Runs are the latest finished runs, newest first.
	Runs []RunBrief `json:"runs"`
}

// RunBrief is the part of a run the next session needs.
type RunBrief struct {
	ID            string            `json:"id"`
	Status        run.Status        `json:"status"`
	Actor         run.ActorSnapshot `json:"actor"`
	LeaseOwner    run.ActorSnapshot `json:"lease_owner"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    *time.Time        `json:"finished_at,omitempty"`
	ExpiresAt     time.Time         `json:"expires_at"`
	LeaseExpired  bool              `json:"lease_expired"`
	ResultSummary string            `json:"result_summary,omitempty"`
}

// Service builds briefs.
type Service struct {
	tasks TaskReader
	runs  RunReader
	now   func() time.Time
}

func NewService(tasks TaskReader, runs RunReader) *Service {
	return &Service{tasks: tasks, runs: runs, now: time.Now}
}

// ForTask builds the brief of one task.
func (s *Service) ForTask(ctx context.Context, selector task.Selector) (Brief, error) {
	show, err := s.tasks.Show(ctx, selector, false)
	if err != nil {
		return Brief{}, err
	}

	runs, err := s.runs.ListRuns(ctx, run.ListOptions{TaskID: show.Task.ID, Limit: runLimit + 1})
	if err != nil {
		return Brief{}, err
	}

	brief := Brief{Task: show.Task, Notes: []task.Event{}, Runs: []RunBrief{}}
	brief.Handoff, brief.Notes = notes(show.Events)

	now := s.now()
	for _, value := range runs {
		summary := runBrief(value, now)
		if value.Status == run.StatusActive {
			brief.ActiveRun = &summary
			continue
		}
		if len(brief.Runs) < runLimit {
			brief.Runs = append(brief.Runs, summary)
		}
	}

	return brief, nil
}

// ResumeInput selects the interrupted work of a project.
type ResumeInput struct {
	ProjectID string

	// ActorID keeps only runs whose lease this actor holds. Empty keeps all.
	ActorID string
}

// Resume builds the briefs of tasks with an active run whose lease is live
// or expired recently, newest run first.
func (s *Service) Resume(ctx context.Context, input ResumeInput) ([]Brief, error) {
	active, err := s.runs.ListRuns(ctx, run.ListOptions{
		ProjectID: input.ProjectID,
		Statuses:  []run.Status{run.StatusActive},
		Limit:     resumeRunLimit,
	})
	if err != nil {
		return nil, err
	}

	cutoff := s.now().Add(-resumeWindow)
	briefs := []Brief{}

	for _, value := range active {
		if input.ActorID != "" && value.LeaseOwner.ID != input.ActorID {
			continue
		}
		if value.ExpiresAt.Before(cutoff) {
			continue
		}

		brief, err := s.ForTask(ctx, task.Selector{ProjectID: input.ProjectID, ID: value.TaskID})
		if err != nil {
			return nil, err
		}
		briefs = append(briefs, brief)
	}

	return briefs, nil
}

// notes returns the latest handoff and up to noteLimit notes written after it.
func notes(events []task.Event) (*task.Event, []task.Event) {
	var handoff *task.Event
	start := 0

	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == task.EventHandoff {
			value := events[i]
			handoff = &value
			start = i + 1
			break
		}
	}

	result := []task.Event{}
	for _, event := range events[start:] {
		switch event.Type {
		case task.EventProgress, task.EventBlocked, task.EventComment:
			result = append(result, event)
		}
	}
	if len(result) > noteLimit {
		result = result[len(result)-noteLimit:]
	}

	return handoff, result
}

func runBrief(value run.Run, now time.Time) RunBrief {
	return RunBrief{
		ID:            value.ID,
		Status:        value.Status,
		Actor:         value.Actor,
		LeaseOwner:    value.LeaseOwner,
		StartedAt:     value.StartedAt,
		FinishedAt:    value.FinishedAt,
		ExpiresAt:     value.ExpiresAt,
		LeaseExpired:  value.Status == run.StatusActive && !value.ExpiresAt.After(now),
		ResultSummary: value.ResultSummary,
	}
}

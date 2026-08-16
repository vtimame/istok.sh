package task

import (
	"strings"
	"time"
)

// Selector identifies a task within a project. Numbers are deliberately never
// globally resolved: ProjectID is required for both number and UUID selectors.
type Selector struct {
	ProjectID string `json:"project_id"`
	ID        string `json:"id,omitempty"`
	Number    int64  `json:"number,omitempty"`
}

func (v Selector) Validate() error {
	if !IsUUIDv7(v.ProjectID) {
		return NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if (v.ID == "") == (v.Number == 0) {
		return NewError(CodeInvalid, "exactly one task selector is required")
	}
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "task id must be a canonical UUIDv7")
	}
	if v.Number < 0 {
		return NewError(CodeInvalid, "task number must be positive")
	}

	return nil
}

type Patch struct {
	Title              *string `json:"title,omitempty"`
	Description        *string `json:"description,omitempty"`
	AcceptanceCriteria *string `json:"acceptance_criteria,omitempty"`
	Notes              *string `json:"notes,omitempty"`
}

func (v Patch) Validate() error {
	if v.Title == nil && v.Description == nil && v.AcceptanceCriteria == nil && v.Notes == nil {
		return NewError(CodeInvalid, "task patch must not be empty")
	}
	if v.Title != nil && strings.TrimSpace(*v.Title) == "" {
		return NewError(CodeInvalid, "task title must not be blank")
	}

	return nil
}

func (v Patch) Apply(value *Task) error {
	if err := v.Validate(); err != nil {
		return err
	}

	if value.DeletedAt != nil {
		return NewError(CodeConflict, "cannot mutate a deleted task")
	}

	if v.Title != nil {
		value.Title = strings.TrimSpace(*v.Title)
	}
	if v.Description != nil {
		value.Description = *v.Description
	}
	if v.AcceptanceCriteria != nil {
		value.AcceptanceCriteria = *v.AcceptanceCriteria
	}
	if v.Notes != nil {
		value.Notes = *v.Notes
	}

	return nil
}

func (v Task) RequireActive() error {
	if v.DeletedAt != nil {
		return NewError(CodeConflict, "cannot mutate a deleted task")
	}

	return nil
}

func (v *Task) Block() error {
	if err := v.RequireActive(); err != nil {
		return err
	}
	if v.Status != StatusOpen {
		return NewError(CodeInvalidTransition, "only open tasks can be blocked")
	}

	v.Status = StatusBlocked
	return nil
}

func (v *Task) Unblock() error {
	if err := v.RequireActive(); err != nil {
		return err
	}
	if v.Status != StatusBlocked {
		return NewError(CodeInvalidTransition, "only blocked tasks can be unblocked")
	}

	v.Status = StatusOpen
	return nil
}

type Dependency struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	BlockerTaskID string    `json:"blocker_task_id"`
	BlockedTaskID string    `json:"blocked_task_id"`
	EdgeType      string    `json:"edge_type"`
	CreatedAt     time.Time `json:"created_at"`
}

type TaskSummary struct {
	ID        string     `json:"id"`
	Number    int64      `json:"number"`
	Status    Status     `json:"status"`
	Title     string     `json:"title"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type BlockerSummary = TaskSummary

type ListOptions struct {
	Statuses       []Status
	IncludeDeleted bool
}

func (v ListOptions) Validate() error {
	for _, status := range v.Statuses {
		if !status.Valid() {
			return NewError(CodeInvalid, "invalid task status")
		}
	}

	return nil
}

func ValidateDependency(blocker, blocked Task) error {
	if err := blocker.RequireActive(); err != nil {
		return err
	}
	if err := blocked.RequireActive(); err != nil {
		return err
	}
	if blocker.ID == blocked.ID {
		return NewError(CodeInvalid, "a task cannot block itself")
	}
	if blocker.ProjectID != blocked.ProjectID {
		return NewError(CodeConflict, "dependencies require tasks from the same project")
	}

	return nil
}

type TaskListItem struct {
	Task
	HasActiveRun   bool             `json:"has_active_run"`
	ActiveBlockers []BlockerSummary `json:"active_blockers,omitempty"`
}

type EffectiveState string

const (
	EffectiveStateReady      EffectiveState = "ready"
	EffectiveStateInProgress EffectiveState = "in_progress"
	EffectiveStateBlocked    EffectiveState = "blocked"
	EffectiveStateDone       EffectiveState = "done"
)

func DeriveState(target Task, hasActiveRun bool, blockers []TaskSummary) EffectiveState {
	if target.Status == StatusDone {
		return EffectiveStateDone
	}
	if hasActiveRun {
		return EffectiveStateInProgress
	}
	if target.Status == StatusBlocked || hasActiveBlocker(blockers) {
		return EffectiveStateBlocked
	}

	return EffectiveStateReady
}

func hasActiveBlocker(blockers []TaskSummary) bool {
	for _, blocker := range blockers {
		if blocker.DeletedAt == nil && blocker.Status != StatusDone {
			return true
		}
	}

	return false
}

type Show struct {
	Task         Task          `json:"task"`
	Events       []Event       `json:"events"`
	Incoming     []Dependency  `json:"incoming"`
	Outgoing     []Dependency  `json:"outgoing"`
	HasActiveRun bool          `json:"has_active_run"`
	Blockers     []TaskSummary `json:"blockers"`
	Dependents   []TaskSummary `json:"dependents"`
}

// Package task contains the transport-independent local task model.
package task

import (
	"strings"
	"time"
)

type Status string

const (
	StatusOpen    Status = "open"
	StatusBlocked Status = "blocked"
	StatusDone    Status = "done"
)

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusBlocked || s == StatusDone
}

type ActorSnapshot struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (a ActorSnapshot) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Kind) == "" || strings.TrimSpace(a.Name) == "" {
		return NewError(CodeInvalid, "actor id, kind, and name are required")
	}

	return nil
}

type Task struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"project_id"`
	Number             int64      `json:"number"`
	Revision           int64      `json:"revision"`
	Status             Status     `json:"status"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	AcceptanceCriteria string     `json:"acceptance_criteria"`
	Notes              string     `json:"notes"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
}

type CreateInput struct {
	ID                 string `json:"id,omitempty"`
	ProjectID          string `json:"project_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Notes              string `json:"notes"`
}

func (v CreateInput) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "task id must be a canonical UUIDv7")
	}

	if !IsUUIDv7(v.ProjectID) {
		return NewError(CodeInvalid, "project id must be a canonical UUIDv7")
	}

	if strings.TrimSpace(v.Title) == "" {
		return NewError(CodeInvalid, "task title is required")
	}

	return nil
}

type Event struct {
	ID           string        `json:"id"`
	TaskID       string        `json:"task_id"`
	Type         string        `json:"type"`
	Body         string        `json:"body"`
	TaskRevision int64         `json:"task_revision"`
	Actor        ActorSnapshot `json:"actor"`
	CreatedAt    time.Time     `json:"created_at"`
}

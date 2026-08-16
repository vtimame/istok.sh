// Package task contains the transport-independent local task model.
package task

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Code string

const (
	CodeInvalid          Code = "invalid_argument"
	CodeNotFound         Code = "task_not_found"
	CodeConflict         Code = "task_conflict"
	CodeRevisionConflict Code = "revision_conflict"
	CodeInternal         Code = "internal_error"
)

type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func ErrorCode(err error) Code {
	var value *Error
	if errors.As(err, &value) {
		return value.Code
	}

	return CodeInternal
}

func NewError(code Code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

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

func NewID() (string, error) {
	value, err := uuid.NewV7()
	if err != nil {
		return "", err
	}

	return value.String(), nil
}

func IsUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

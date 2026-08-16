package task

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalid           Code = "invalid_argument"
	CodeNotFound          Code = "task_not_found"
	CodeConflict          Code = "task_conflict"
	CodeRevisionConflict  Code = "revision_conflict"
	CodeInvalidTransition Code = "invalid_transition"
	CodeDependencyCycle   Code = "dependency_cycle"
	CodeHasActiveRun      Code = "task_has_active_run"
	CodeInternal          Code = "internal_error"
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

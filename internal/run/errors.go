package run

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalid           Code = "invalid"
	CodeNotFound          Code = "not_found"
	CodeConflict          Code = "conflict"
	CodeRevisionConflict  Code = "revision_conflict"
	CodeActiveRunExists   Code = "active_run_exists"
	CodeInvalidTransition Code = "invalid_transition"
	CodeEvidenceRequired  Code = "evidence_required"
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

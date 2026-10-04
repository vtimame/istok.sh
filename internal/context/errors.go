package context

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalid          Code = "invalid_argument"
	CodeNotFound         Code = "context_not_found"
	CodeConflict         Code = "context_conflict"
	CodeRevisionConflict Code = "revision_conflict"
	CodeInternal         Code = "internal_error"
)

type Error struct {
	Code    Code
	Message string
	Budget  *BudgetFailure
}

type BudgetFailure struct {
	Reason                string   `json:"reason"`
	Lane                  string   `json:"lane,omitempty"`
	Action                string   `json:"action"`
	RequiredItems         int      `json:"required_items,omitempty"`
	MaxItems              int      `json:"max_items,omitempty"`
	RequiredBytes         int      `json:"required_bytes,omitempty"`
	MaxBytes              int      `json:"max_bytes,omitempty"`
	RecordIDs             []string `json:"record_ids"`
	SuggestedContextLimit int      `json:"suggested_context_limit,omitempty"`
}

const (
	BudgetReasonDurableItems         = "durable_items"
	BudgetReasonAbsoluteDurableItems = "absolute_durable_items"
	BudgetReasonDurableBytes         = "durable_bytes"
	BudgetReasonRecordBytes          = "record_bytes"
)

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

func NewBudgetError(message string, budget BudgetFailure) error {
	budget.RecordIDs = append([]string{}, budget.RecordIDs...)

	return &Error{Code: CodeConflict, Message: message, Budget: &budget}
}

func ErrorBudget(err error) *BudgetFailure {
	var value *Error
	if !errors.As(err, &value) || value.Budget == nil {
		return nil
	}

	budget := *value.Budget
	budget.RecordIDs = append([]string{}, value.Budget.RecordIDs...)

	return &budget
}

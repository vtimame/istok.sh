package run

import "strings"

func (v Run) Validate() error {
	if !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "run id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.TaskID) {
		return NewError(CodeInvalid, "task id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.ContextSnapshotID) {
		return NewError(CodeInvalid, "context snapshot id must be a canonical UUIDv7")
	}
	if !v.Status.Valid() {
		return NewError(CodeInvalid, "run status is invalid")
	}

	return nil
}

func (v Run) IsActive() bool {
	return v.Status == StatusActive
}

func (v Run) IsTerminal() bool {
	return v.Status.Terminal()
}

func (v *Run) TransitionTo(status Status) error {
	if !status.Valid() {
		return NewError(CodeInvalid, "run status is invalid")
	}
	if v.IsTerminal() {
		return NewError(CodeInvalidTransition, "cannot transition a terminal run")
	}
	if v.Status == status {
		return nil
	}

	v.Status = status
	return nil
}

func (v Execution) Validate() error {
	if v.ID != "" && !IsUUIDv7(v.ID) {
		return NewError(CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	if !IsUUIDv7(v.RunID) {
		return NewError(CodeInvalid, "run id must be a canonical UUIDv7")
	}
	if len(v.Argv) == 0 {
		return NewError(CodeInvalid, "argv is required")
	}
	for _, arg := range v.Argv {
		if strings.TrimSpace(arg) == "" {
			return NewError(CodeInvalid, "argv entries must be non-blank")
		}
	}
	if strings.TrimSpace(v.CWD) == "" {
		return NewError(CodeInvalid, "cwd is required")
	}
	if !v.Status.Valid() {
		return NewError(CodeInvalid, "execution status is invalid")
	}

	return nil
}

func (v Validation) Validate() error {
	if !IsUUIDv7(v.ExecutionID) {
		return NewError(CodeInvalid, "execution id must be a canonical UUIDv7")
	}
	if !v.Source.Valid() {
		return NewError(CodeInvalid, "validation source is invalid")
	}
	if !v.Status.Valid() {
		return NewError(CodeInvalid, "validation status is invalid")
	}
	if strings.TrimSpace(v.Command) == "" {
		return NewError(CodeInvalid, "command is required")
	}
	if strings.TrimSpace(v.Summary) == "" {
		return NewError(CodeInvalid, "summary is required")
	}

	return nil
}

func (v Completion) Validate() error {
	if v.Note == "" {
		return NewError(CodeInvalid, "note is required")
	}
	if !IsUUIDv7(v.TaskID) {
		return NewError(CodeInvalid, "task id must be a canonical UUIDv7")
	}

	return nil
}

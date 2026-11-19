package run

import "strings"

// MaxEvidenceOutputTail bounds the command output kept with evidence.
const MaxEvidenceOutputTail = 4 << 10

// ManualCommand names evidence that has no command, such as a manual
// end-to-end check or a CI pipeline status.
const ManualCommand = "manual"

// RecordEvidenceInput records a check made outside Istok in one call: a
// record-only execution and an attested validation on the run.
type RecordEvidenceInput struct {
	ExecutionID  string           `json:"-"`
	ValidationID string           `json:"-"`
	RunID        string           `json:"run_id"`
	LeaseID      string           `json:"lease_id"`
	CWD          string           `json:"-"`
	Status       ValidationStatus `json:"status"`
	Summary      string           `json:"summary"`
	Command      string           `json:"command,omitempty"`
	ExitCode     *int             `json:"exit_code,omitempty"`
	DurationMS   *int64           `json:"duration_ms,omitempty"`
	OutputTail   string           `json:"output_tail,omitempty"`
}

func (v RecordEvidenceInput) Validate() error {
	if !IsUUIDv7(v.ExecutionID) || !IsUUIDv7(v.ValidationID) {
		return NewError(CodeInvalid, "evidence execution and validation ids must be canonical UUIDv7")
	}
	if !IsUUIDv7(v.RunID) || !IsUUIDv7(v.LeaseID) {
		return NewError(CodeInvalid, "run id and lease id are required")
	}
	if strings.TrimSpace(v.CWD) == "" {
		return NewError(CodeInvalid, "cwd is required")
	}
	if !v.Status.Valid() {
		return NewError(CodeInvalid, "status must be passed or failed")
	}
	if strings.TrimSpace(v.Summary) == "" {
		return NewError(CodeInvalid, "summary is required")
	}
	if v.DurationMS != nil && *v.DurationMS < 0 {
		return NewError(CodeInvalid, "duration must not be negative")
	}

	return nil
}

// EvidenceCommand is the command the evidence names, or ManualCommand.
func (v RecordEvidenceInput) EvidenceCommand() string {
	if command := strings.TrimSpace(v.Command); command != "" {
		return command
	}

	return ManualCommand
}

// EvidenceSummary is the summary with the bounded output tail appended.
func (v RecordEvidenceInput) EvidenceSummary() string {
	summary := strings.TrimSpace(v.Summary)

	tail := strings.TrimSpace(v.OutputTail)
	if tail == "" {
		return summary
	}
	if len(tail) > MaxEvidenceOutputTail {
		tail = tail[len(tail)-MaxEvidenceOutputTail:]
	}

	return summary + "\n\nOutput tail:\n" + tail
}

// Evidence is the record-only execution and the validation made from it.
type Evidence struct {
	Execution  Execution  `json:"execution"`
	Validation Validation `json:"validation"`
}

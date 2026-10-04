package cli

import (
	"encoding/json"
	"strings"

	"github.com/vtimame/istok.sh/internal/run"
)

// historyBody is a task event body split into the run it refers to and the
// text meant for people. Run events store machine references in their body
// (see internal/storage/runrepo); the web UI parses them the same way in
// web/src/lib/task-events.ts.
//
//	claimed        "<run id>" or JSON {run_id, without_retrieval, override_reason, context_override_reason, ...}
//	run_finished   "<run id>\n<result summary>"
//	run_abandoned  "run_id=<run id>\n<reason>"
//	completed      "<completion id>\n<note>" (the completion id is not shown)
type historyBody struct {
	RunID string
	Text  string
}

type claimEventBody struct {
	RunID                 string `json:"run_id"`
	WithoutRetrieval      bool   `json:"without_retrieval"`
	OverrideReason        string `json:"override_reason"`
	ContextOverrideReason string `json:"context_override_reason"`
}

func parseHistoryBody(kind, body string) historyBody {
	switch kind {
	case "claimed":
		return parseClaimBody(body)

	case "run_finished":
		first, rest := splitFirstLine(body)
		if run.IsUUIDv7(first) {
			return historyBody{RunID: first, Text: rest}
		}

	case "run_abandoned":
		first, rest := splitFirstLine(body)
		if id := strings.TrimPrefix(first, "run_id="); run.IsUUIDv7(id) {
			return historyBody{RunID: id, Text: rest}
		}

	case "completed":
		first, rest := splitFirstLine(body)
		if run.IsUUIDv7(first) {
			return historyBody{Text: rest}
		}
	}

	return historyBody{Text: body}
}

func parseClaimBody(body string) historyBody {
	trimmed := strings.TrimSpace(body)
	if run.IsUUIDv7(trimmed) {
		return historyBody{RunID: trimmed}
	}

	var value claimEventBody
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil || !run.IsUUIDv7(value.RunID) {
		return historyBody{Text: body}
	}

	var notes []string
	if value.WithoutRetrieval && value.OverrideReason != "" {
		notes = append(notes, "Claimed without code retrieval: "+value.OverrideReason)
	}
	if value.ContextOverrideReason != "" {
		notes = append(notes, "All context included: "+value.ContextOverrideReason)
	}

	return historyBody{RunID: value.RunID, Text: strings.Join(notes, "\n")}
}

func splitFirstLine(body string) (string, string) {
	first, rest, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(first), strings.TrimSpace(rest)
}

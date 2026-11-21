package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

// historyBody is a task event body split into the run it refers to and the
// text meant for people. Run events store machine references in their body
// (see internal/storage/runrepo).
//
//	claimed        "<run id>" or JSON {run_id, without_retrieval, override_reason, context_override_reason, ...}
//	run_finished   "<run id>\n<result summary>"
//	run_abandoned  "run_id=<run id>\n<reason>"
//	completed      "<completion id>\n<note>" (the completion id is not shown)
//	commit         JSON task.Commit, shown as "<short sha> <subject>" and the repository
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

	case task.EventCommit:
		if commit, ok := task.CommitOf(task.Event{Type: kind, Body: body}); ok {
			return historyBody{Text: commitText(commit)}
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

// shortSHA is how many characters of a commit sha history shows.
const shortSHA = 12

func commitText(commit task.Commit) string {
	sha := commit.SHA
	if len(sha) > shortSHA {
		sha = sha[:shortSHA]
	}

	text := fmt.Sprintf("`%s` %s", sha, commit.Subject)
	if commit.Repository != "" && commit.Repository != "." {
		text += "\nRepository: " + commit.Repository
	}

	return text
}

// linkedCommits returns the commits linked to the task.
func linkedCommits(events []task.Event) []task.Commit {
	var commits []task.Commit
	for _, event := range events {
		if commit, ok := task.CommitOf(event); ok {
			commits = append(commits, commit)
		}
	}

	return commits
}

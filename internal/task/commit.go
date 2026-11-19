package task

import (
	"encoding/json"
	"strings"
	"time"
)

// EventCommit links a git commit to the task. Its body is a Commit in JSON.
const EventCommit = "commit"

// Commit is a git commit made for a task.
type Commit struct {
	// Repository is the repository folder relative to the project root; "."
	// is the root itself.
	Repository string `json:"repository"`

	SHA         string    `json:"sha"`
	Subject     string    `json:"subject"`
	CommittedAt time.Time `json:"committed_at"`

	// Remote is the origin URL, when the repository has one.
	Remote string `json:"remote,omitempty"`
}

// Validate checks the fields a linked commit needs.
func (c Commit) Validate() error {
	if strings.TrimSpace(c.Repository) == "" || !isCommitSHA(c.SHA) {
		return NewError(CodeInvalid, "commit repository and a full hexadecimal sha are required")
	}

	return nil
}

// Body encodes the commit as an event body.
func (c Commit) Body() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}

// CommitOf decodes a commit event; ok is false for other events.
func CommitOf(event Event) (Commit, bool) {
	if event.Type != EventCommit {
		return Commit{}, false
	}

	var value Commit
	if err := json.Unmarshal([]byte(event.Body), &value); err != nil {
		return Commit{}, false
	}

	return value, true
}

// isCommitSHA accepts full SHA-1 and SHA-256 object names.
func isCommitSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}

	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}

	return true
}

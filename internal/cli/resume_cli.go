package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	handoffapp "github.com/vtimame/istok.sh/internal/application/handoff"
	"github.com/vtimame/istok.sh/internal/project"
)

// ResumeCommand prints the interrupted work of the current project, for an
// agent hook after a restart or a context compaction.
type ResumeCommand struct {
	Actor    string `name:"actor" help:"Only runs whose lease this actor ID holds. Defaults to every agent." env:"ISTOK_AGENT_ID"`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

// resumeReport is the result of istok resume. Outside a registered project it
// is empty, so a hook prints nothing.
type resumeReport struct {
	Registered bool               `json:"registered"`
	Project    *project.Project   `json:"project,omitempty"`
	Tasks      []handoffapp.Brief `json:"tasks"`
}

func runResume(ctx context.Context, command ResumeCommand, cwd string, output io.Writer) error {
	report, err := loadResume(ctx, command.Database, cwd, command.Actor)

	if command.JSON {
		if err != nil {
			return jsonError(err)
		}

		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string       `json:"schema_version"`
			Result        resumeReport `json:"result"`
		}{"1", report})
	}
	if err != nil {
		return err
	}

	text := renderResumeReport(report)
	if text == "" {
		return nil
	}

	_, err = fmt.Fprintln(output, text)

	return err
}

// renderResumeReport writes plain text without terminal styling: a hook
// passes it to the agent as context.
func renderResumeReport(report resumeReport) string {
	if !report.Registered {
		return ""
	}
	if len(report.Tasks) == 0 {
		return fmt.Sprintf("Istok project %q: no interrupted work.", report.Project.Name)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "Istok project %q: interrupted work. Continue a task with task_start and its task_id.\n", report.Project.Name)

	for _, brief := range report.Tasks {
		output.WriteString("\n")
		fmt.Fprintf(&output, "Task #%d %s\n", brief.Task.Number, brief.Task.Title)
		fmt.Fprintf(&output, "  task_id: %s\n", brief.Task.ID)

		if brief.ActiveRun != nil {
			lease := "live until " + localTime(brief.ActiveRun.ExpiresAt)
			if brief.ActiveRun.LeaseExpired {
				lease = "expired at " + localTime(brief.ActiveRun.ExpiresAt)
			}
			fmt.Fprintf(&output, "  run: %s by %s, lease %s\n", brief.ActiveRun.ID, brief.ActiveRun.LeaseOwner.Name, lease)
		}

		if brief.Handoff != nil {
			fmt.Fprintf(&output, "  handoff (%s, %s):\n", localTime(brief.Handoff.CreatedAt), brief.Handoff.Actor.Name)
			output.WriteString(indent(brief.Handoff.Body, "    "))
		}

		if len(brief.Notes) > 0 {
			output.WriteString("  notes since then:\n")
			for _, note := range brief.Notes {
				fmt.Fprintf(&output, "    - %s %s: %s\n", localTime(note.CreatedAt), note.Type, oneLine(note.Body))
			}
		}

		for _, previous := range brief.Runs {
			fmt.Fprintf(&output, "  earlier run %s: %s", previous.ID, previous.Status)
			if previous.ResultSummary != "" {
				fmt.Fprintf(&output, ": %s", oneLine(previous.ResultSummary))
			}
			output.WriteString("\n")
		}
	}

	return strings.TrimRight(output.String(), "\n")
}

func localTime(value time.Time) string {
	return value.Local().Format("2006-01-02 15:04")
}

func indent(value, prefix string) string {
	var output strings.Builder
	for _, line := range strings.Split(strings.TrimRight(value, "\n"), "\n") {
		output.WriteString(prefix)
		output.WriteString(line)
		output.WriteString("\n")
	}

	return output.String()
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/agenthooks"
	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	handoffapp "github.com/vtimame/istok.sh/internal/application/handoff"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/project"
)

// HookCommand runs one Istok hook for an agent. Agents call it; people use
// `istok hooks install`.
type HookCommand struct {
	Agent    string `arg:"" help:"Agent: claude or codex."`
	Event    string `arg:"" help:"Event: session-start, pre-compact or stop."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
}

// hookInput is the part of the agent's hook input Istok reads. Claude Code
// and Codex share these field names.
type hookInput struct {
	CWD            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`

	// ToolName and ToolInput come with post-tool-use.
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// maxHookInput bounds the hook input read from stdin.
const maxHookInput = 1 << 20

// hookTimeout keeps a hook well inside the agent's own timeout.
const hookTimeout = 5 * time.Second

// runHook never fails the agent: problems go to stderr and the hook exits 0
// with no context, or with an empty JSON object for stop.
func runHook(ctx context.Context, command HookCommand, cwd string, input io.Reader, output, errorOutput io.Writer) error {
	agent, err := agenthooks.ParseAgent(command.Agent)
	if err != nil {
		return err
	}
	event, err := agenthooks.ParseEvent(command.Event)
	if err != nil {
		return err
	}

	text, err := hookOutput(ctx, command, event, cwd, input)
	if err != nil {
		fmt.Fprintf(errorOutput, "istok hook %s %s: %v\n", agent, event, err)
		text = ""
	}

	if event == agenthooks.EventStop && text == "" {
		text = "{}"
	}
	if text == "" {
		return nil
	}

	_, err = fmt.Fprintln(output, text)

	return err
}

func hookOutput(ctx context.Context, command HookCommand, event agenthooks.Event, cwd string, input io.Reader) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	var value hookInput
	data, err := io.ReadAll(io.LimitReader(input, maxHookInput))
	if err != nil {
		return "", fmt.Errorf("read hook input: %w", err)
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &value); err != nil {
			return "", fmt.Errorf("parse hook input: %w", err)
		}
	}
	if value.CWD != "" {
		cwd = value.CWD
	}
	if event == agenthooks.EventStop && value.StopHookActive {
		return "", nil
	}
	if event == agenthooks.EventPostToolUse {
		return linkCommitHook(ctx, command.Database, cwd, value)
	}

	report, err := loadResume(ctx, command.Database, cwd, os.Getenv("ISTOK_AGENT_ID"))
	if err != nil {
		return "", err
	}
	if !report.Registered || len(report.Tasks) == 0 {
		return "", nil
	}

	switch event {
	case agenthooks.EventSessionStart:
		return renderResumeReport(report), nil
	case agenthooks.EventPreCompact:
		return preCompactInstructions + "\n\n" + renderResumeReport(report), nil
	case agenthooks.EventStop:
		return stopDecision(report)
	}

	return "", nil
}

// preCompactInstructions becomes part of the compaction prompt.
const preCompactInstructions = `Istok tracks the work below outside this conversation. In the summary, keep for each task its number, title, task_id and where the work stands, and state the next step: after compaction call task_start with that task_id to continue the run and read its handoff, and call task_handoff before stopping.`

// stopDecision asks the agent once to record a handoff for active runs whose
// handoff is out of date.
func stopDecision(report resumeReport) (string, error) {
	var stale []string
	for _, brief := range report.Tasks {
		if brief.ActiveRun == nil || brief.ActiveRun.LeaseExpired || !brief.NeedsHandoff() {
			continue
		}
		stale = append(stale, fmt.Sprintf("#%d %q (task_id %s)", brief.Task.Number, brief.Task.Title, brief.Task.ID))
	}
	if len(stale) == 0 {
		return "", nil
	}

	reason := "Istok: " + strings.Join(stale, ", ") + " has an active run without a current handoff. If it is your task, call task_handoff with what is done, where you stopped, decisions and the next step, then finish your reply."
	encoded, err := json.Marshal(struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}{Decision: "block", Reason: reason})
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}

// loadResume builds the resume report of the project at cwd.
func loadResume(ctx context.Context, database, cwd, actorID string) (resumeReport, error) {
	var report resumeReport

	err := withTaskServices(ctx, database, func(projects *project.Service, tasks *taskapp.Service, runs *runapp.Service) error {
		value, err := resumeAt(ctx, projects, handoffapp.NewService(tasks, runs), cwd, actorID)
		report = value

		return err
	})

	return report, err
}

// withTaskServices runs action with the task services of the database.
func withTaskServices(ctx context.Context, database string, action func(*project.Service, *taskapp.Service, *runapp.Service) error) error {
	var projects *project.Service
	var tasks *taskapp.Service
	var runs *runapp.Service

	app := fx.New(
		fx.NopLogger,
		bootstrap.TaskOptions(database),
		fx.Invoke(func(projectService *project.Service, taskService *taskapp.Service, runService *runapp.Service) {
			projects, tasks, runs = projectService, taskService, runService
		}),
	)
	if err := app.Start(ctx); err != nil {
		return err
	}

	err := action(projects, tasks, runs)

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return errors.Join(err, app.Stop(stopCtx))
}

// resumeAt is the resume report of the project at cwd; outside a registered
// project it is empty.
func resumeAt(ctx context.Context, projects *project.Service, briefs *handoffapp.Service, cwd, actorID string) (resumeReport, error) {
	current, err := projects.Current(ctx, cwd)
	if err != nil {
		if project.ErrorCode(err) == project.CodeNotFound {
			return resumeReport{Tasks: []handoffapp.Brief{}}, nil
		}

		return resumeReport{}, err
	}

	values, err := briefs.Resume(ctx, handoffapp.ResumeInput{ProjectID: current.ID, ActorID: actorID})
	if err != nil {
		return resumeReport{}, err
	}

	return resumeReport{Registered: true, Project: &current, Tasks: values}, nil
}

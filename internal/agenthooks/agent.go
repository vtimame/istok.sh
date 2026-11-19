// Package agenthooks installs Istok hooks into the settings of coding agents
// and describes what each hook does. A hook runs `istok hook AGENT EVENT`,
// which prints the project's interrupted work for the agent's context.
package agenthooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Agent is a coding agent Istok can install hooks for.
type Agent string

const (
	Claude Agent = "claude"
	Codex  Agent = "codex"
)

// Agents lists the supported agents.
var Agents = []Agent{Claude, Codex}

// ParseAgent validates an agent name.
func ParseAgent(value string) (Agent, error) {
	for _, agent := range Agents {
		if string(agent) == value {
			return agent, nil
		}
	}

	return "", fmt.Errorf("unsupported agent %q: use claude or codex", value)
}

// Scope is where the hooks are installed.
type Scope string

const (
	// ScopeUser covers every project of the user. Outside a registered
	// project the hooks print nothing.
	ScopeUser Scope = "user"

	// ScopeProject covers one project.
	ScopeProject Scope = "project"
)

// Event is what triggers a hook, in Istok's terms.
type Event string

const (
	// EventSessionStart prints the interrupted work when a session starts,
	// resumes or continues after a compaction.
	EventSessionStart Event = "session-start"

	// EventPreCompact tells the compaction summary to keep the Istok state.
	EventPreCompact Event = "pre-compact"

	// EventStop asks once for a handoff when the agent stops with an active
	// run whose handoff is out of date.
	EventStop Event = "stop"

	// EventPostToolUse links a commit the agent just made to its task.
	EventPostToolUse Event = "post-tool-use"
)

// allEvents lists every event Istok handles.
var allEvents = []Event{EventSessionStart, EventPreCompact, EventPostToolUse, EventStop}

// ParseEvent validates an event name.
func ParseEvent(value string) (Event, error) {
	for _, event := range allEvents {
		if string(event) == value {
			return event, nil
		}
	}

	return "", fmt.Errorf("unsupported hook event %q: use session-start, pre-compact, stop or post-tool-use", value)
}

// Hook is one hook entry in an agent's settings.
type Hook struct {
	Event Event

	// Name is the agent's event name, such as SessionStart.
	Name string

	// Matcher limits the hook; empty matches every trigger.
	Matcher string
}

// hookTimeoutSeconds bounds each Istok hook. A hook reads the local database
// only, so it finishes well within it.
const hookTimeoutSeconds = 10

// Hooks returns the hooks Istok installs for the agent. Stop is opt-in.
// Codex ignores the stdout of PreCompact, so it gets no compaction hook; its
// SessionStart after a compaction carries the state instead. Commits are
// linked after Claude Code's Bash tool; Codex gets no commit hook yet.
func Hooks(agent Agent, withStop bool) []Hook {
	hooks := []Hook{{Event: EventSessionStart, Name: "SessionStart"}}
	if agent == Claude {
		hooks = append(hooks,
			Hook{Event: EventPreCompact, Name: "PreCompact"},
			Hook{Event: EventPostToolUse, Name: "PostToolUse", Matcher: "Bash"},
		)
	}
	if withStop {
		hooks = append(hooks, Hook{Event: EventStop, Name: "Stop"})
	}

	return hooks
}

// SettingsPath returns the settings file that holds the agent's hooks.
// projectRoot is used only for ScopeProject.
func SettingsPath(agent Agent, scope Scope, projectRoot string) (string, error) {
	switch scope {
	case ScopeUser:
		home, err := agentHome(agent)
		if err != nil {
			return "", err
		}
		if agent == Claude {
			return filepath.Join(home, "settings.json"), nil
		}

		return filepath.Join(home, "hooks.json"), nil
	case ScopeProject:
		if projectRoot == "" {
			return "", fmt.Errorf("project scope needs a registered Istok project")
		}
		if agent == Claude {
			// settings.local.json is the personal, untracked layer.
			return filepath.Join(projectRoot, ".claude", "settings.local.json"), nil
		}

		return filepath.Join(projectRoot, ".codex", "hooks.json"), nil
	}

	return "", fmt.Errorf("unsupported scope %q: use user or project", scope)
}

// agentHome is the agent's configuration directory, honoring the same
// environment variables the agents do.
func agentHome(agent Agent) (string, error) {
	variable := "CLAUDE_CONFIG_DIR"
	directory := ".claude"
	if agent == Codex {
		variable = "CODEX_HOME"
		directory = ".codex"
	}

	if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}

	return filepath.Join(home, directory), nil
}

// Command is the shell command a hook runs.
func Command(executable string, agent Agent, event Event) string {
	return fmt.Sprintf("%s hook %s %s", shellQuote(executable), agent, event)
}

// isIstokCommand tells whether a hook command was installed by Istok for the
// agent, whatever path the executable had.
func isIstokCommand(command string, agent Agent) bool {
	fields := strings.Fields(command)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i+1] != "hook" || fields[i+2] != string(agent) {
			continue
		}
		if strings.Contains(filepath.Base(strings.Trim(fields[i], `'"`)), "istok") {
			return true
		}
	}

	return false
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

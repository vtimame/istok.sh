package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/agenthooks"
	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	"github.com/vtimame/istok.sh/internal/project"
)

// HooksCommand installs Istok hooks into coding agents.
type HooksCommand struct {
	Install   HooksInstallCommand   `cmd:"" help:"Install Istok hooks into an agent's settings."`
	Uninstall HooksUninstallCommand `cmd:"" help:"Remove Istok hooks from an agent's settings."`
	Status    HooksStatusCommand    `cmd:"" help:"Show where Istok hooks are installed."`
}

type HooksInstallCommand struct {
	Agent    string `arg:"" enum:"claude,codex" help:"Agent: claude or codex."`
	Scope    string `name:"scope" default:"user" enum:"user,project" help:"user: every project (the hooks are silent outside Istok projects); project: the current project only."`
	Stop     bool   `name:"stop" help:"Also ask the agent once for task_handoff when it stops with an active run whose handoff is out of date."`
	Print    bool   `name:"print" help:"Print the settings to add instead of writing them."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type HooksUninstallCommand struct {
	Agent    string `arg:"" enum:"claude,codex" help:"Agent: claude or codex."`
	Scope    string `name:"scope" default:"user" enum:"user,project" help:"user or project."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type HooksStatusCommand struct {
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

// hooksLocation is one settings file Istok may write hooks into.
type hooksLocation struct {
	Agent  agenthooks.Agent   `json:"agent"`
	Scope  agenthooks.Scope   `json:"scope"`
	Path   string             `json:"path"`
	Events []agenthooks.Event `json:"events"`
}

func runHooks(ctx context.Context, command HooksCommand, commandName, cwd string, output io.Writer) error {
	switch {
	case strings.HasPrefix(commandName, "hooks install"):
		return writeHooksResult(output, command.Install.JSON, func() (any, error) { return installHooks(ctx, command.Install, cwd, output) })
	case strings.HasPrefix(commandName, "hooks uninstall"):
		return writeHooksResult(output, command.Uninstall.JSON, func() (any, error) { return uninstallHooks(ctx, command.Uninstall, cwd) })
	default:
		return writeHooksResult(output, command.Status.JSON, func() (any, error) { return hooksStatus(ctx, command.Status, cwd) })
	}
}

func installHooks(ctx context.Context, command HooksInstallCommand, cwd string, output io.Writer) (any, error) {
	agent, err := agenthooks.ParseAgent(command.Agent)
	if err != nil {
		return nil, err
	}

	executable, err := hookExecutable()
	if err != nil {
		return nil, err
	}
	hooks := agenthooks.Hooks(agent, command.Stop)

	if command.Print {
		snippet, err := agenthooks.Snippet(agent, hooks, executable)
		if err != nil {
			return nil, err
		}
		if command.JSON {
			return json.RawMessage(snippet), nil
		}

		_, err = output.Write(snippet)
		return nil, err
	}

	path, err := hooksPath(ctx, agent, agenthooks.Scope(command.Scope), command.Database, cwd)
	if err != nil {
		return nil, err
	}

	return agenthooks.Install(path, agent, hooks, executable)
}

func uninstallHooks(ctx context.Context, command HooksUninstallCommand, cwd string) (any, error) {
	agent, err := agenthooks.ParseAgent(command.Agent)
	if err != nil {
		return nil, err
	}

	path, err := hooksPath(ctx, agent, agenthooks.Scope(command.Scope), command.Database, cwd)
	if err != nil {
		return nil, err
	}

	return agenthooks.Uninstall(path, agent)
}

func hooksStatus(ctx context.Context, command HooksStatusCommand, cwd string) (any, error) {
	root, err := projectRoot(ctx, command.Database, cwd)
	if err != nil && project.ErrorCode(err) != project.CodeNotFound {
		return nil, err
	}

	locations := []hooksLocation{}
	for _, agent := range agenthooks.Agents {
		for _, scope := range []agenthooks.Scope{agenthooks.ScopeUser, agenthooks.ScopeProject} {
			if scope == agenthooks.ScopeProject && root == "" {
				continue
			}

			path, err := agenthooks.SettingsPath(agent, scope, root)
			if err != nil {
				return nil, err
			}
			events, err := agenthooks.Installed(path, agent)
			if err != nil {
				return nil, err
			}
			locations = append(locations, hooksLocation{Agent: agent, Scope: scope, Path: path, Events: events})
		}
	}

	return locations, nil
}

func hooksPath(ctx context.Context, agent agenthooks.Agent, scope agenthooks.Scope, database, cwd string) (string, error) {
	root := ""
	if scope == agenthooks.ScopeProject {
		value, err := projectRoot(ctx, database, cwd)
		if err != nil {
			return "", err
		}
		root = value
	}

	return agenthooks.SettingsPath(agent, scope, root)
}

// projectRoot is the root of the registered project at cwd.
func projectRoot(ctx context.Context, database, cwd string) (string, error) {
	var projects *project.Service

	app := fx.New(
		fx.NopLogger,
		bootstrap.ProjectOptions(database),
		fx.Invoke(func(service *project.Service) { projects = service }),
	)
	if err := app.Start(ctx); err != nil {
		return "", err
	}

	current, err := projects.Current(ctx, cwd)

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := errors.Join(err, app.Stop(stopCtx)); err != nil {
		return "", err
	}
	if current.Root == nil {
		return "", &project.Error{Code: project.CodeNotFound, Message: "the current project has no folder on this device"}
	}

	return current.Root.CanonicalPath, nil
}

// hookExecutable finds the binary hooks run; tests replace it.
var hookExecutable = istokExecutable

// istokExecutable is the command hooks run: the istok found on PATH when it
// is this binary, so a symlinked install keeps working after updates, or the
// path of this binary.
func istokExecutable() (string, error) {
	current, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find the istok executable: %w", err)
	}

	if found, err := exec.LookPath("istok"); err == nil {
		foundInfo, foundErr := os.Stat(found)
		currentInfo, currentErr := os.Stat(current)
		if foundErr == nil && currentErr == nil && os.SameFile(foundInfo, currentInfo) {
			return found, nil
		}
	}

	return current, nil
}

func writeHooksResult(output io.Writer, jsonOutput bool, action func() (any, error)) error {
	value, err := action()

	if jsonOutput {
		if err != nil {
			return jsonError(err)
		}

		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{"1", value})
	}
	if err != nil {
		return err
	}

	text := renderHooksValue(value)
	if text == "" {
		return nil
	}

	_, err = fmt.Fprintln(output, text)

	return err
}

func renderHooksValue(value any) string {
	switch typed := value.(type) {
	case agenthooks.Change:
		return renderHooksChange(typed)
	case []hooksLocation:
		return renderHooksStatus(typed)
	}

	return ""
}

func renderHooksChange(change agenthooks.Change) string {
	var output strings.Builder

	if !change.Changed {
		fmt.Fprintf(&output, "No changes: %s", change.Path)
	} else {
		fmt.Fprintf(&output, "Updated %s", change.Path)
		if change.Backup != "" {
			fmt.Fprintf(&output, "\nBackup: %s", change.Backup)
		}
	}

	if len(change.Events) == 0 {
		output.WriteString("\nIstok hooks: none")
	} else {
		fmt.Fprintf(&output, "\nIstok hooks: %s", joinEvents(change.Events))
	}

	if change.Changed && len(change.Events) > 0 && strings.Contains(change.Path, ".codex") {
		output.WriteString("\nCodex runs new hooks after you review and trust them: open /hooks in Codex.")
	}

	return output.String()
}

func renderHooksStatus(locations []hooksLocation) string {
	var output strings.Builder

	for i, location := range locations {
		if i > 0 {
			output.WriteString("\n")
		}

		events := "not installed"
		if len(location.Events) > 0 {
			events = joinEvents(location.Events)
		}
		fmt.Fprintf(&output, "%-6s %-7s %s: %s", location.Agent, location.Scope, location.Path, events)
	}

	return output.String()
}

func joinEvents(events []agenthooks.Event) string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, string(event))
	}

	return strings.Join(names, ", ")
}

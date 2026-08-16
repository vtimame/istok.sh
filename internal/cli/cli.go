package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	"s26.dev/istok-cli/internal/application/bootstrap"
	updateapp "s26.dev/istok-cli/internal/application/update"
	"s26.dev/istok-cli/internal/buildinfo"
	"s26.dev/istok-cli/internal/cli/presentation"
	"s26.dev/istok-cli/internal/mcpserver"
	"s26.dev/istok-cli/internal/project"
)

type CLI struct {
	Version    VersionCommand         `cmd:"" help:"Print version information."`
	MCP        MCPCommand             `cmd:"" help:"Run the MCP server over standard input/output."`
	Update     UpdateCommand          `cmd:"" help:"Check for and securely install a new CLI release."`
	Init       InitCommand            `cmd:"" help:"Initialize the current directory or PATH as a local project."`
	Project    ProjectCommand         `cmd:"" help:"Show and manage local projects."`
	Task       TaskCommand            `cmd:"" help:"Show local tasks for the current project."`
	Completion CompletionCommand      `cmd:"" help:"Set up shell completion."`
	Migrate    InternalMigrateCommand `cmd:"" hidden:""`
	Cleanup    InternalCleanupCommand `cmd:"" hidden:""`
}

type UpdateCommand struct {
	Check    bool   `help:"Only check for an update; do not change files."`
	Yes      bool   `help:"Install without an interactive confirmation."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
}

type InternalMigrateCommand struct {
	Database string `name:"database" required:""`
}

type InternalCleanupCommand struct {
	PID  int      `name:"pid" required:""`
	Path []string `name:"path" required:""`
}

type VersionCommand struct{}

type MCPCommand struct {
	Database string            `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	Profile  mcpserver.Profile `name:"profile" enum:"worker,admin" default:"worker" help:"MCP tool profile."`
}

type InitCommand struct {
	Path     string `arg:"" optional:"" default:"." help:"Directory to initialize; defaults to the current directory."`
	Name     string `name:"name" help:"Project name; defaults to the directory name."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type ProjectCommand struct {
	Show    ProjectShowCommand    `cmd:"" help:"Show one local project."`
	List    ProjectListCommand    `cmd:"" help:"List local projects."`
	Rename  ProjectRenameCommand  `cmd:"" help:"Rename a local project."`
	Rebind  ProjectRebindCommand  `cmd:"" help:"Change a project's root path."`
	Delete  ProjectDeleteCommand  `cmd:"" help:"Move a local project to the recoverable trash."`
	Restore ProjectRestoreCommand `cmd:"" help:"Restore a deleted local project."`
}

type ProjectShowCommand struct {
	Selector string `arg:"" optional:"" help:"Project ID, unambiguous ID prefix, or name; defaults to the current project."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type ProjectListCommand struct {
	Deleted  bool   `name:"deleted" help:"Include deleted projects."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type ProjectRenameCommand struct {
	Name     string `arg:"" help:"New project name."`
	Selector string `name:"project" help:"Project ID, unambiguous ID prefix, or name; defaults to the current project."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type ProjectRebindCommand struct {
	Selector string `arg:"" help:"Project ID, unambiguous ID prefix, or name to rebind."`
	Path     string `arg:"" optional:"" default:"." help:"New root path; defaults to the current directory."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

type ProjectDeleteCommand struct {
	Selector string `name:"project" help:"Project ID, unambiguous ID prefix, or name; defaults to the current project."`
	Yes      bool   `name:"yes" help:"Move to local trash without an interactive confirmation."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response; requires --yes."`
}

type ProjectRestoreCommand struct {
	Selector string `arg:"" help:"Deleted project ID, unambiguous ID prefix, or name to restore."`
	Path     string `arg:"" optional:"" help:"Root path to restore; omit to reuse the previous root."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

func Execute(ctx context.Context, args []string, output, errorOutput io.Writer) error {
	return ExecuteWithInput(ctx, args, os.Stdin, output, errorOutput)
}

func ExecuteWithInput(ctx context.Context, args []string, input io.Reader, output, errorOutput io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	return ExecuteAt(ctx, args, input, output, errorOutput, cwd)
}

// ExecuteAt runs the command using an explicitly captured working directory.
// Tests use it to avoid process-wide directory changes.
func ExecuteAt(ctx context.Context, args []string, input io.Reader, output, errorOutput io.Writer, cwd string) error {
	var command CLI
	helpShown := false
	parser, err := kong.New(
		&command,
		kong.Name("istok"),
		kong.Writers(output, errorOutput),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact:             true,
			NoExpandSubcommands: true,
			FlagsLast:           true,
			WrapUpperBound:      100,
		}),
		kong.Exit(func(int) { helpShown = true }),
	)
	if err != nil {
		return fmt.Errorf("configure CLI parser: %w", err)
	}
	completionHandled, err := registerCompletion(parser)
	if err != nil {
		return err
	}
	if completionHandled {
		return nil
	}

	parsed, err := parser.Parse(args)
	if err != nil {
		if helpShown {
			return nil
		}

		return err
	}
	if helpShown {
		return nil
	}

	commandName := parsed.Command()
	switch {
	case commandName == "version":
		return runVersion(ctx, output)
	case strings.HasPrefix(commandName, "completion"):
		return command.Completion.Run(parsed)
	case commandName == "mcp":
		root, err := project.Canonicalize(cwd)
		if err != nil {
			return fmt.Errorf("resolve MCP root: %w", err)
		}

		return runMCP(ctx, command.MCP.Database, root.CanonicalPath, command.MCP.Profile)
	case strings.HasPrefix(commandName, "init"):
		return runProject(ctx, command.Init.Database, command.Init.JSON, output, func(service *project.Service) (any, error) {
			return service.Init(ctx, resolvePath(cwd, command.Init.Path), command.Init.Name)
		})
	case strings.HasPrefix(commandName, "project show"):
		return runProject(ctx, command.Project.Show.Database, command.Project.Show.JSON, output, projectAction(ctx, cwd, command.Project.Show.Selector, func(s *project.Service, selector string) (any, error) { return s.Resolve(ctx, selector, false) }))
	case commandName == "project list":
		return runProject(ctx, command.Project.List.Database, command.Project.List.JSON, output, func(s *project.Service) (any, error) { return s.List(ctx, command.Project.List.Deleted) })
	case strings.HasPrefix(commandName, "project rename"):
		return runProject(ctx, command.Project.Rename.Database, command.Project.Rename.JSON, output, projectAction(ctx, cwd, command.Project.Rename.Selector, func(s *project.Service, selector string) (any, error) {
			return s.Rename(ctx, selector, command.Project.Rename.Name, nil)
		}))
	case strings.HasPrefix(commandName, "project rebind"):
		return runProject(ctx, command.Project.Rebind.Database, command.Project.Rebind.JSON, output, func(s *project.Service) (any, error) {
			return s.Rebind(ctx, command.Project.Rebind.Selector, resolvePath(cwd, command.Project.Rebind.Path), nil)
		})
	case commandName == "project delete":
		if !command.Project.Delete.Yes {
			if command.Project.Delete.JSON {
				return jsonError(&project.Error{Code: project.CodeInvalid, Message: "project deletion requires --yes"})
			}

			fmt.Fprint(output, "Delete local project? [y/N] ")
			answer, err := bufio.NewReader(input).ReadString('\n')
			if err != nil && len(answer) == 0 {
				return fmt.Errorf("read project deletion confirmation: %w", err)
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				_, err := fmt.Fprintln(output, "Project deletion cancelled.")
				return err
			}
		}
		return runProject(ctx, command.Project.Delete.Database, command.Project.Delete.JSON, output, projectAction(ctx, cwd, command.Project.Delete.Selector, func(s *project.Service, selector string) (any, error) { return s.Delete(ctx, selector, nil) }))
	case strings.HasPrefix(commandName, "project restore"):
		return runProject(ctx, command.Project.Restore.Database, command.Project.Restore.JSON, output, func(s *project.Service) (any, error) {
			path := command.Project.Restore.Path
			if path != "" {
				path = resolvePath(cwd, path)
			}

			return s.Restore(ctx, command.Project.Restore.Selector, path, nil)
		})
	case commandName == "task list":
		return runTaskList(ctx, command.Task.List.Database, cwd, output)
	case strings.HasPrefix(commandName, "task show"):
		return runTaskShow(ctx, command.Task.Show, cwd, output)
	case commandName == "update":
		return runUpdate(ctx, command.Update, input, output)
	case commandName == "migrate":
		return runMigrations(ctx, command.Migrate.Database)
	case commandName == "cleanup":
		return updateapp.CleanupAfterParentExit(command.Cleanup.PID, command.Cleanup.Path)
	default:
		return fmt.Errorf("unsupported command %q", parsed.Command())
	}
}

func runUpdate(ctx context.Context, command UpdateCommand, input io.Reader, output io.Writer) error {
	var result error
	app := bootstrap.UpdateApp(ctx, updateapp.Command{Check: command.Check, Yes: command.Yes, Database: command.Database}, input, output, &result)
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start update application: %w", err)
	}
	if err := app.Stop(ctx); err != nil {
		return fmt.Errorf("stop update application: %w", err)
	}
	return result
}

func runMigrations(ctx context.Context, database string) error {
	var dbApp = bootstrap.MigrationApp(database)
	if err := dbApp.Start(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return dbApp.Stop(ctx)
}

func runVersion(ctx context.Context, output io.Writer) error {
	app := bootstrap.VersionApp(output)

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start version command: %w", err)
	}

	if err := app.Stop(ctx); err != nil {
		return fmt.Errorf("stop version command: %w", err)
	}

	return nil
}

func runMCP(ctx context.Context, database, root string, profile mcpserver.Profile) error {
	var server *mcp.Server
	app := bootstrap.MCPApp(database, root, profile, &server)

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start MCP application: %w", err)
	}

	return runWithShutdown(ctx,
		func(ctx context.Context) error { return server.Run(ctx, &mcp.StdioTransport{}) },
		app.Stop,
	)
}

func projectAction(ctx context.Context, cwd, selector string, fn func(*project.Service, string) (any, error)) func(*project.Service) (any, error) {
	return func(s *project.Service) (any, error) {
		if selector != "" {
			return fn(s, selector)
		}
		p, err := s.Current(ctx, cwd)
		if err != nil {
			return nil, err
		}
		return fn(s, p.ID)
	}
}

func resolvePath(cwd, path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(cwd, path)
}

func runProject(ctx context.Context, database string, jsonOutput bool, output io.Writer, action func(*project.Service) (any, error)) error {
	var service *project.Service
	app := fx.New(fx.NopLogger, bootstrap.ProjectOptions(database), fx.Invoke(func(s *project.Service) { service = s }))
	if err := app.Start(ctx); err != nil {
		if jsonOutput {
			return jsonError(err)
		}

		return err
	}

	value, err := action(service)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop project application: %w", stopErr)
	}

	if jsonOutput {
		if err != nil || stopErr != nil {
			return jsonError(errors.Join(err, stopErr))
		}

		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{"1", value})
	}
	if err != nil {
		return errors.Join(err, stopErr)
	}
	_, err = fmt.Fprintln(output, renderProjectValue(value))
	return errors.Join(err, stopErr)
}

type jsonCommandError struct {
	value string
	cause error
}

func (e jsonCommandError) Error() string { return e.value }
func (e jsonCommandError) Unwrap() error { return e.cause }

func jsonError(err error) error {
	encoded, marshalErr := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code    project.Code `json:"code"`
			Message string       `json:"message"`
		} `json:"error"`
	}{SchemaVersion: "1", Error: struct {
		Code    project.Code `json:"code"`
		Message string       `json:"message"`
	}{Code: project.ErrorCode(err), Message: err.Error()}})
	if marshalErr != nil {
		return err
	}

	return jsonCommandError{value: string(encoded), cause: err}
}

func renderProjectValue(value any) string {
	switch typed := value.(type) {
	case project.InitResult:
		if typed.Created {
			return "Initialized " + renderProjectValue(typed.Project)
		}
		return "Already initialized " + renderProjectValue(typed.Project)
	case project.Project:
		status := "active"
		if typed.DeletedAt != nil {
			status = "deleted"
		}

		if typed.Root != nil {
			return fmt.Sprintf("%s  %s  %s  revision=%d  %s", typed.ID, typed.Name, status, typed.Revision, typed.Root.CanonicalPath)
		}

		return fmt.Sprintf("%s  %s  %s  revision=%d", typed.ID, typed.Name, status, typed.Revision)
	case []project.Project:
		rows := make([][]string, 0, len(typed))
		for _, item := range typed {
			status := "active"
			if item.DeletedAt != nil {
				status = "deleted"
			}

			root := "—"
			if item.Root != nil {
				root = item.Root.CanonicalPath
			}

			rows = append(rows, []string{item.ID, item.Name, status, fmt.Sprint(item.Revision), root})
		}
		if len(rows) == 0 {
			rows = append(rows, []string{"—", "No projects.", "—", "—", "—"})
		}

		return presentation.RenderTable([]string{"ID", "NAME", "STATUS", "REVISION", "ROOT"}, rows)
	default:
		return fmt.Sprint(value)
	}
}

func runWithShutdown(ctx context.Context, run func(context.Context) error, stop func(context.Context) error) error {
	runErr := run(ctx)
	if errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
		runErr = nil
	} else if runErr != nil {
		runErr = fmt.Errorf("run MCP server: %w", runErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := stop(shutdownCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop MCP application: %w", stopErr)
	}

	if runErr != nil || stopErr != nil {
		return errors.Join(runErr, stopErr)
	}

	return nil
}

func ValidateMCPGraph(path string) error {
	return fx.ValidateApp(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		bootstrap.ProjectOptions(path),
		mcpserver.Module(mcpserver.Config{Profile: mcpserver.Worker, Root: "."}),
		fx.Invoke(func(*mcp.Server) {}),
	)
}

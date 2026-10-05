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

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	updateapp "github.com/vtimame/istok.sh/internal/application/update"
	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/exchange"
	"github.com/vtimame/istok.sh/internal/mcpserver"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/updater"
)

type CLI struct {
	Version    VersionCommand         `cmd:"" help:"Print version information."`
	MCP        MCPCommand             `cmd:"" help:"Run the MCP server over standard input/output."`
	Update     UpdateCommand          `cmd:"" help:"Check for and securely install a new CLI release."`
	UI         UICommand              `cmd:"" name:"ui" help:"Open the local web UI or manage it as a service."`
	Init       InitCommand            `cmd:"" help:"Initialize the current directory or PATH as a local project."`
	Project    ProjectCommand         `cmd:"" help:"Show and manage local projects."`
	Task       TaskCommand            `cmd:"" help:"Inspect tasks in the current project."`
	Run        RunCommand             `cmd:"" help:"Manage local runs and execution evidence."`
	Context    ContextCommand         `cmd:"" help:"Manage saved project context."`
	Knowledge  KnowledgeCommand       `cmd:"" help:"Manage pull-first project knowledge outside the repository."`
	Export     ExportCommand          `cmd:"" help:"Export projects to a file for another device."`
	Import     ImportCommand          `cmd:"" help:"Import projects exported on another device."`
	Index      IndexCommand           `cmd:"" help:"Inspect and rebuild the local code index."`
	Search     SearchCommand          `cmd:"" help:"Search the local code index."`
	Graph      GraphCommand           `cmd:"" help:"Explore the local code graph."`
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
	Database  string            `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	Profile   mcpserver.Profile `name:"profile" enum:"worker,supervisor,admin" default:"worker" help:"MCP tool profile."`
	ActorID   string            `name:"actor-id" help:"Stable MCP agent ID; generated once at startup when omitted." env:"ISTOK_AGENT_ID"`
	ActorName string            `name:"actor-name" default:"MCP Agent" help:"MCP agent display name." env:"ISTOK_AGENT_NAME"`
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
		kong.Help(taskHelpPrinter),
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
	case commandName == "ui" || strings.HasPrefix(commandName, "ui "):
		return runUICommand(ctx, command.UI, commandName, output)
	case commandName == "mcp":
		root, err := project.Canonicalize(cwd)
		if err != nil {
			return fmt.Errorf("resolve MCP root: %w", err)
		}

		return runMCP(ctx, command.MCP.Database, root.CanonicalPath, command.MCP.Profile, command.MCP.ActorID, command.MCP.ActorName)
	case strings.HasPrefix(commandName, "init"):
		return runInit(ctx, command.Init, cwd, output)
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
	case strings.HasPrefix(commandName, "task claim"):
		return runTaskClaim(ctx, command.Task.Claim, cwd, output)
	case strings.HasPrefix(commandName, "task done"):
		return runTaskDone(ctx, command.Task.Done, cwd, output)
	case strings.HasPrefix(commandName, "task"):
		return runTaskCommand(ctx, command.Task, commandName, cwd, output)
	case strings.HasPrefix(commandName, "run"):
		return runRunCommand(ctx, command.Run, commandName, cwd, output)
	case strings.HasPrefix(commandName, "context"):
		return runContext(ctx, command.Context, commandName, cwd, input, output)
	case strings.HasPrefix(commandName, "knowledge"):
		return runKnowledge(ctx, command.Knowledge, commandName, cwd, output)
	case commandName == "export":
		return runExport(ctx, command.Export, cwd, output)
	case strings.HasPrefix(commandName, "import"):
		return runImport(ctx, command.Import, cwd, input, output)
	case commandName == "index status":
		return runIndexStatus(ctx, command.Index.Status, cwd, output)
	case commandName == "index rebuild":
		return runIndexRebuild(ctx, command.Index.Rebuild, cwd, output)
	case strings.HasPrefix(commandName, "search"):
		return runIndexSearch(ctx, command.Search, cwd, output)
	case strings.HasPrefix(commandName, "graph"):
		return runGraph(ctx, command.Graph, commandName, cwd, output)
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
		var unavailable *updater.UnavailableError
		if errors.As(err, &unavailable) {
			return fmt.Errorf("%w\nReinstall an official release to enable updates: curl -fsSL https://get.istok.sh | sh", unavailable)
		}

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

func runMCP(ctx context.Context, database, root string, profile mcpserver.Profile, actorID, actorName string) error {
	var server *mcp.Server
	app := bootstrap.MCPApp(database, root, profile, actorID, actorName, &server)

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

	return runProjectApplication(ctx, jsonOutput, output, app, func() (any, error) {
		return action(service)
	})
}

func runInit(ctx context.Context, command InitCommand, cwd string, output io.Writer) error {
	var projectService *project.Service
	var indexService *indexingapp.Service
	app := fx.New(
		fx.NopLogger,
		bootstrap.InitOptions(command.Database, os.Getenv("ISTOK_INDEX_ROOT")),
		fx.Invoke(func(projects *project.Service, indexes *indexingapp.Service) {
			projectService = projects
			indexService = indexes
		}),
	)

	return runProjectApplication(ctx, command.JSON, output, app, func() (any, error) {
		result, err := projectService.Init(ctx, resolvePath(cwd, command.Path), command.Name)
		if err != nil {
			return nil, err
		}

		if _, err := indexService.EnsureFresh(ctx, result.Project); err != nil {
			return nil, err
		}

		return result, nil
	})
}

func runProjectApplication(ctx context.Context, jsonOutput bool, output io.Writer, app *fx.App, action func() (any, error)) error {
	if err := app.Start(ctx); err != nil {
		if jsonOutput {
			return jsonError(err)
		}

		return err
	}

	value, err := action()
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
	if value == nil {
		return stopErr
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
	}{Code: commandErrorCode(err), Message: err.Error()}})
	if marshalErr != nil {
		return err
	}

	return jsonCommandError{value: string(encoded), cause: err}
}

func commandErrorCode(err error) project.Code {
	if indexingapp.IsError(err) {
		return project.Code(indexingapp.ErrorCode)
	}
	if code, ok := exchange.ErrorCode(err); ok {
		return project.Code(code)
	}

	return project.ErrorCode(err)
}

func renderProjectValue(value any) string {
	switch typed := value.(type) {
	case project.InitResult:
		result := renderInitResult(typed.Project)
		header := presentation.SectionTitle("Init")

		if typed.Created {
			return fmt.Sprintf("%s\n\n%s", header, result)
		}

		return fmt.Sprintf("%s\n\n%s\n%s", header, presentation.Metadata("Already initialized"), result)
	case project.Project:
		return renderProject(typed)
	case []project.Project:
		return renderProjectList(typed)
	case exportSummary:
		return renderExportSummary(typed)
	case exchange.Report:
		return renderImportReport(typed)
	default:
		return fmt.Sprint(value)
	}
}

func renderProject(project project.Project) string {
	status := "active"
	if project.DeletedAt != nil {
		status = "deleted"
	}

	root := "not on this device"
	if project.Root != nil {
		root = project.Root.CanonicalPath
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf(
		"%s\n\n%s %s %s %s\n\n%s\n\n%s",
		presentation.SectionTitle("Project"),
		presentation.Brand(project.Name),
		presentation.StyledStatus(status),
		presentation.Neutral("·"),
		presentation.Metadata(fmt.Sprintf("revision %d", project.Revision)),
		presentation.RailLine(presentation.Path(root)),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("ID"), presentation.Warning(project.ID))),
	))

	return output.String()
}

func renderInitResult(project project.Project) string {
	status := "active"
	if project.DeletedAt != nil {
		status = "deleted"
	}

	root := "not on this device"
	if project.Root != nil {
		root = project.Root.CanonicalPath
	}

	return fmt.Sprintf(
		"%s\n%s\n%s\n%s",
		presentation.RailLine(fmt.Sprintf("Project %s %s", presentation.StyledStatus(status), presentation.Metadata(fmt.Sprintf("revision %d", project.Revision)))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Name"), presentation.Brand(project.Name))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Root"), presentation.Path(root))),
		presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("ID"), presentation.Warning(project.ID))),
	)
}

func renderProjectList(projects []project.Project) string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Local projects"))
	output.WriteString("\n\n")

	if len(projects) == 0 {
		output.WriteString(presentation.Metadata("No local projects."))
		output.WriteString("\n")
		return output.String()
	}

	rows := make([][]string, 0, len(projects))
	for _, item := range projects {
		status := "active"
		if item.DeletedAt != nil {
			status = "deleted"
		}

		root := "not on this device"
		if item.Root != nil {
			root = item.Root.CanonicalPath
		}

		rows = append(rows, []string{presentation.BrandStrong(item.Name), presentation.StyledStatus(status), presentation.Neutral(root)})
	}

	output.WriteString(presentation.RenderUnboundedTable([]string{"NAME", "STATE", "ROOT"}, rows))
	output.WriteString("\n\n")
	output.WriteString(presentation.Neutral(fmt.Sprintf("  %d total projects", len(projects))))

	return output.String()
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
		bootstrap.MCPOptions(path),
		mcpserver.Module(mcpserver.Config{Profile: mcpserver.Worker, Root: "."}),
		fx.Invoke(func(*mcp.Server) {}),
	)
}

package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	contextapp "s26.dev/istok-cli/internal/application/context"
	indexingapp "s26.dev/istok-cli/internal/application/indexing"
	runapp "s26.dev/istok-cli/internal/application/run"
	runworkflow "s26.dev/istok-cli/internal/application/runworkflow"
	taskapp "s26.dev/istok-cli/internal/application/task"
	updateapp "s26.dev/istok-cli/internal/application/update"
	"s26.dev/istok-cli/internal/artifactstore"
	"s26.dev/istok-cli/internal/buildinfo"
	"s26.dev/istok-cli/internal/mcpserver"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/storage"
	"s26.dev/istok-cli/internal/storage/contextrepo"
	"s26.dev/istok-cli/internal/storage/projectrepo"
	"s26.dev/istok-cli/internal/storage/runrepo"
	"s26.dev/istok-cli/internal/storage/taskrepo"
	"s26.dev/istok-cli/internal/updater"
)

func VersionApp(output io.Writer) *fx.App {
	return fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		fx.Invoke(func(info buildinfo.Info) error {
			_, err := fmt.Fprintln(output, info.Version)
			return err
		}),
	)
}

// UpdateApp is intentionally independent from the MCP and storage Fx graphs.
func UpdateApp(ctx context.Context, command updateapp.Command, input io.Reader, output io.Writer, result *error) *fx.App {
	return fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		fx.Provide(func(info buildinfo.Info) (*updater.Service, error) { return updater.New(info) }),
		fx.Invoke(func(info buildinfo.Info, service *updater.Service) {
			*result = updateapp.Application{Service: service, Version: info.Version, Input: input, Output: output}.Run(ctx, command)
		}),
	)
}

func ValidateUpdateGraph() error {
	return fx.ValidateApp(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		fx.Provide(func(info buildinfo.Info) (*updater.Service, error) { return updater.New(info) }),
		fx.Invoke(func(*updater.Service) {}),
	)
}

func ProjectOptions(path string) fx.Option {
	return fx.Options(
		storage.Module(storage.Config{Path: path}),
		fx.Provide(projectrepo.New),
		fx.Provide(func(repository *projectrepo.Repository) project.Repository { return repository }),
		fx.Provide(project.NewService),
	)
}

func InitOptions(path, indexRoot string) fx.Option {
	return fx.Options(
		ProjectOptions(path),
		IndexingOptions(indexRoot),
	)
}

func IndexingOptions(indexRoot string) fx.Option {
	return fx.Options(
		fx.Supply(indexingapp.Config{IndexRoot: indexRoot}),
		fx.Provide(indexingapp.NewService),
	)
}

func TaskOptions(path string) fx.Option {
	return kernelOptions(path)
}

func RunWorkflowOptions(path string) fx.Option {
	return fx.Options(
		kernelOptions(path),
		fx.Provide(func() (*artifactstore.Store, error) {
			database := path
			if database == "" {
				database = storage.DefaultPath()
			}
			if database == ":memory:" {
				return nil, fmt.Errorf("managed run workflow requires a file-backed database")
			}

			absolute, err := filepath.Abs(database)
			if err != nil {
				return nil, fmt.Errorf("resolve artifact database path: %w", err)
			}

			return artifactstore.New(filepath.Join(filepath.Dir(absolute), "artifacts"))
		}),
		fx.Provide(runworkflow.NewService),
	)
}

func kernelOptions(path string) fx.Option {
	return fx.Options(
		ProjectOptions(path),
		fx.Provide(taskrepo.New),
		fx.Provide(func(repository *taskrepo.Repository) taskapp.Repository { return repository }),
		fx.Provide(func(repository *taskrepo.Repository) runapp.TaskResolver { return repository }),
		fx.Provide(contextrepo.New),
		fx.Provide(func(repository *contextrepo.Repository) contextapp.Repository { return repository }),
		fx.Provide(contextapp.NewService),
		fx.Provide(func(service *contextapp.Service) runapp.ContextBuilder { return service }),
		fx.Provide(runrepo.New),
		fx.Provide(func(repository *runrepo.Repository) runapp.Repository { return repository }),
		fx.Provide(func(repository *runrepo.Repository) taskapp.ActiveRunInspector { return repository }),
		fx.Provide(runapp.NewService),
		fx.Provide(taskapp.NewService),
	)
}

func ContextOptions(path string) fx.Option {
	return fx.Options(
		ProjectOptions(path),
		fx.Provide(contextrepo.New),
		fx.Provide(func(repository *contextrepo.Repository) contextapp.Repository { return repository }),
		fx.Provide(contextapp.NewService),
	)
}

func MCPOptions(path string) fx.Option {
	return fx.Options(
		RunWorkflowOptions(path),
		fx.Provide(func(db *sql.DB) mcpserver.HealthChecker { return db }),
	)
}

func MCPApp(path, root string, profile mcpserver.Profile, actorID, actorName string, server **mcp.Server) *fx.App {
	return fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		MCPOptions(path),
		mcpserver.Module(mcpserver.Config{Profile: profile, Root: root, ActorID: actorID, ActorName: actorName}),
		fx.Invoke(func(value *mcp.Server) { *server = value }),
	)
}

// MigrationApp opens only storage, so the post-update child cannot start MCP
// infrastructure while it applies the embedded Goose migrations.
func MigrationApp(path string) *fx.App {
	return fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: path}),
		fx.Invoke(func(*sql.DB) {}),
	)
}

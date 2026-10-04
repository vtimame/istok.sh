package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	contextapp "github.com/vtimame/istok.sh/internal/application/context"
	contextpackapp "github.com/vtimame/istok.sh/internal/application/contextpack"
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	runworkflow "github.com/vtimame/istok.sh/internal/application/runworkflow"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	updateapp "github.com/vtimame/istok.sh/internal/application/update"
	"github.com/vtimame/istok.sh/internal/artifactstore"
	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/mcpserver"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/storage"
	"github.com/vtimame/istok.sh/internal/storage/contextrepo"
	"github.com/vtimame/istok.sh/internal/storage/knowledgerepo"
	"github.com/vtimame/istok.sh/internal/storage/projectrepo"
	"github.com/vtimame/istok.sh/internal/storage/runrepo"
	"github.com/vtimame/istok.sh/internal/storage/taskrepo"
	"github.com/vtimame/istok.sh/internal/storage/uireadrepo"
	"github.com/vtimame/istok.sh/internal/updater"
	"github.com/vtimame/istok.sh/internal/webui"
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
		IndexingOptions(os.Getenv("ISTOK_INDEX_ROOT")),
		fx.Provide(taskrepo.New),
		fx.Provide(func(repository *taskrepo.Repository) taskapp.Repository { return repository }),
		fx.Provide(func(repository *taskrepo.Repository) runapp.TaskResolver { return repository }),
		fx.Provide(contextrepo.New),
		fx.Provide(func(repository *contextrepo.Repository) contextapp.Repository { return repository }),
		fx.Provide(contextapp.NewService),
		fx.Provide(knowledgerepo.New),
		fx.Provide(func(repository *knowledgerepo.Repository) knowledgeapp.Repository { return repository }),
		fx.Provide(knowledgeapp.NewService),
		fx.Provide(func(service *knowledgeapp.Service) contextpackapp.KnowledgeCatalog { return service }),
		fx.Provide(func(service *project.Service) contextpackapp.ProjectResolver { return service }),
		fx.Provide(func(service *indexingapp.Service) contextpackapp.TaskRetriever { return service }),
		fx.Provide(contextpackapp.NewService),
		fx.Provide(func(service *contextpackapp.Service) runapp.ContextPackageBuilder { return service }),
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

func KnowledgeOptions(path string) fx.Option {
	return fx.Options(
		ProjectOptions(path),
		fx.Provide(knowledgerepo.New),
		fx.Provide(func(repository *knowledgerepo.Repository) knowledgeapp.Repository { return repository }),
		fx.Provide(knowledgeapp.NewService),
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

// UIApp wires the read-only services behind the embedded web UI.
func UIApp(path string, services *webui.Services) *fx.App {
	return fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		kernelOptions(path),
		fx.Provide(uireadrepo.New),
		fx.Invoke(func(info buildinfo.Info, readModel *uireadrepo.Repository, projects *project.Service, tasks *taskapp.Service, runs *runapp.Service, knowledge *knowledgeapp.Service) {
			*services = webui.Services{Build: info, ReadModel: readModel, Projects: projects, Tasks: tasks, Runs: runs, Knowledge: knowledge}
		}),
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

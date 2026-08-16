package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	updateapp "s26.dev/istok-cli/internal/application/update"
	"s26.dev/istok-cli/internal/buildinfo"
	"s26.dev/istok-cli/internal/mcpserver"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/storage"
	"s26.dev/istok-cli/internal/storage/projectrepo"
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

func MCPApp(path, root string, profile mcpserver.Profile, server **mcp.Server) *fx.App {
	return fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		ProjectOptions(path),
		mcpserver.Module(mcpserver.Config{Profile: profile, Root: root}),
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

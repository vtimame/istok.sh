package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	exchangeapp "github.com/vtimame/istok.sh/internal/application/exchange"
	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/exchange"
)

// ExportCommand writes projects to a bundle file for another device.
type ExportCommand struct {
	Project  []string `name:"project" short:"p" help:"Project to export: ID, unambiguous ID prefix or name. Repeat for several."`
	All      bool     `name:"all" help:"Export every project instead of the current one."`
	Output   string   `name:"output" short:"o" help:"File to write; '-' writes the bundle to standard output. Defaults to istok-export-<time>.json."`
	Database string   `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool     `name:"json" help:"Write a versioned JSON summary."`
}

// ImportCommand merges a bundle exported on another device.
type ImportCommand struct {
	File     string `arg:"" help:"Bundle file; '-' reads standard input."`
	DryRun   bool   `name:"dry-run" help:"Show what would change without writing anything."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON report."`
}

// cliActor attributes history written by CLI commands such as an import.
var cliActor = exchangeapp.Actor{ID: "cli", Kind: "cli", Name: "CLI"}

type exportSummary struct {
	File     string                    `json:"file"`
	Projects []exchange.ProjectSummary `json:"projects"`
}

func runExport(ctx context.Context, command ExportCommand, cwd string, output io.Writer) error {
	if command.Output == "-" && command.JSON {
		return fmt.Errorf("--json writes a summary and cannot be combined with --output -")
	}

	service, app := exchangeApplication(command.Database)

	return runProjectApplication(ctx, command.JSON, output, app, func() (any, error) {
		bundle, err := (*service).Export(ctx, exchangeapp.Selection{All: command.All, Selectors: command.Project, Cwd: cwd})
		if err != nil {
			return nil, err
		}

		if command.Output == "-" {
			return nil, exchangeapp.Encode(output, bundle)
		}

		path := command.Output
		if path == "" {
			path = fmt.Sprintf("istok-export-%s.json", time.Now().Format("20060102-150405"))
		}
		path = resolvePath(cwd, path)

		if err := writeBundle(path, bundle); err != nil {
			return nil, err
		}

		return exportSummary{File: path, Projects: bundle.Projects}, nil
	})
}

// writeBundle writes through a temporary file, so a failed export never
// leaves a truncated bundle behind. The bundle holds project history, so it
// is private to the user.
func writeBundle(path string, bundle exchange.Bundle) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create bundle file: %w", err)
	}
	defer os.Remove(temporary.Name())

	if err := exchangeapp.Encode(temporary, bundle); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set bundle permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close bundle file: %w", err)
	}

	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("write bundle file: %w", err)
	}

	return nil
}

func runImport(ctx context.Context, command ImportCommand, cwd string, input io.Reader, output io.Writer) error {
	service, app := exchangeApplication(command.Database)

	return runProjectApplication(ctx, command.JSON, output, app, func() (any, error) {
		source := input
		if command.File != "-" {
			file, err := os.Open(resolvePath(cwd, command.File))
			if err != nil {
				return nil, exchange.Errorf(exchange.CodeInvalid, "open bundle: %v", err)
			}
			defer file.Close()
			source = file
		}

		bundle, err := exchangeapp.Decode(source)
		if err != nil {
			return nil, err
		}

		return (*service).Import(ctx, bundle, cliActor, command.DryRun)
	})
}

func exchangeApplication(database string) (**exchangeapp.Service, *fx.App) {
	service := new(*exchangeapp.Service)
	app := fx.New(
		fx.NopLogger,
		bootstrap.ExchangeOptions(database),
		fx.Invoke(func(value *exchangeapp.Service) { *service = value }),
	)

	return service, app
}

func renderExportSummary(summary exportSummary) string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Export"))
	output.WriteString("\n")

	for _, value := range summary.Projects {
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s  %s", value.Name, presentation.Metadata(fmt.Sprintf("%d tasks", value.Tasks)))))
		output.WriteString("\n")
	}

	output.WriteString(presentation.RailLine(presentation.Key("File") + " " + presentation.Path(summary.File)))
	output.WriteString("\n")
	output.WriteString(presentation.Metadata("Copy the file to the other device and run: istok import " + filepath.Base(summary.File)))

	return output.String()
}

func renderImportReport(report exchange.Report) string {
	var output strings.Builder

	title := "Import"
	if report.DryRun {
		title = "Import (dry run, nothing was written)"
	}
	output.WriteString(presentation.SectionTitle(title))
	output.WriteString("\n")

	for _, value := range report.Projects {
		state := "merged"
		if value.Created {
			state = "new"
		}
		folder := "folder bound"
		if !value.Bound {
			folder = "no folder on this device"
		}
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s  %s", value.Name, presentation.Metadata(state+" · "+folder))))
		output.WriteString("\n")
	}

	if rows := importTableRows(report.Tables); len(rows) > 0 {
		output.WriteString("\n")
		output.WriteString(presentation.SectionTitle("Changes"))
		output.WriteString("\n")
		output.WriteString(presentation.RenderTable([]string{"TABLE", "NEW", "UPDATED", "SAME", "KEPT LOCAL", "SKIPPED"}, rows))
		output.WriteString("\n")
	}

	if len(report.Renumbered) > 0 {
		output.WriteString("\n")
		output.WriteString(presentation.SectionTitle("Renumbered"))
		output.WriteString("\n")
		for _, value := range report.Renumbered {
			output.WriteString(presentation.RailLine(fmt.Sprintf("%s → %s  %s",
				presentation.StyledNumber(fmt.Sprintf("#%d", value.From)), presentation.StyledNumber(fmt.Sprintf("#%d", value.To)), value.Title)))
			output.WriteString("\n")
		}
	}

	if len(report.Conflicts) > 0 {
		output.WriteString("\n")
		output.WriteString(presentation.SectionTitle("Conflicts (local version kept)"))
		output.WriteString("\n")
		for _, value := range report.Conflicts {
			output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s  %s", value.Table, presentation.Metadata(value.ID), value.Reason)))
			output.WriteString("\n")
		}
	}

	for _, warning := range report.Warnings {
		output.WriteString("\n")
		output.WriteString(presentation.Warning(warning))
	}

	return strings.TrimRight(output.String(), "\n")
}

// importTableRows lists tables with any change, in a stable order.
func importTableRows(tables map[string]exchange.TableCounts) [][]string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := [][]string{}
	for _, name := range names {
		counts := tables[name]
		if counts == (exchange.TableCounts{}) {
			continue
		}
		rows = append(rows, []string{
			name,
			fmt.Sprint(counts.Created),
			fmt.Sprint(counts.Updated),
			fmt.Sprint(counts.Unchanged),
			fmt.Sprint(counts.KeptLocal),
			fmt.Sprint(counts.Skipped),
		})
	}
	return rows
}

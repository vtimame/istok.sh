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

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/codegraph"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

const indexSchemaVersion = "1"

type indexStatusResult struct {
	Project project.Project    `json:"project"`
	Status  indexingapp.Status `json:"status"`
}

type indexSearchResult struct {
	Project         project.Project          `json:"project"`
	Status          indexingapp.Status       `json:"status"`
	ContractVersion string                   `json:"contract_version"`
	Results         []retrieval.SearchResult `json:"results"`
}

type graphSymbolsResult struct {
	Project         project.Project    `json:"project"`
	Status          indexingapp.Status `json:"status"`
	ContractVersion string             `json:"contract_version"`
	Symbols         []codegraph.Node   `json:"symbols"`
}

type graphNeighborsResult struct {
	Project         project.Project             `json:"project"`
	Status          indexingapp.Status          `json:"status"`
	ContractVersion string                      `json:"contract_version"`
	Neighbors       []indexingapp.GraphNeighbor `json:"neighbors"`
}

type graphPathsResult struct {
	Project         project.Project         `json:"project"`
	Status          indexingapp.Status      `json:"status"`
	ContractVersion string                  `json:"contract_version"`
	Paths           []indexingapp.GraphPath `json:"paths"`
}

func runIndexStatus(ctx context.Context, command IndexStatusCommand, cwd string, output io.Writer) error {
	return runIndexApplication(ctx, command.Database, command.JSON, cwd, output, func(indexes *indexingapp.Service, current project.Project) (any, error) {
		status, err := indexes.Status(ctx, current)
		if err != nil {
			return nil, err
		}

		return indexStatusResult{Project: current, Status: status}, nil
	})
}

func runIndexRebuild(ctx context.Context, command IndexRebuildCommand, cwd string, output io.Writer) error {
	return runIndexApplication(ctx, command.Database, command.JSON, cwd, output, func(indexes *indexingapp.Service, current project.Project) (any, error) {
		status, err := indexes.Rebuild(ctx, current)
		if err != nil {
			return nil, err
		}

		return indexStatusResult{Project: current, Status: status}, nil
	})
}

func runIndexSearch(ctx context.Context, command SearchCommand, cwd string, output io.Writer) error {
	return runIndexApplication(ctx, command.Database, command.JSON, cwd, output, func(indexes *indexingapp.Service, current project.Project) (any, error) {
		results, status, err := indexes.Search(ctx, current, retrieval.SearchRequest{Query: command.Query, Limit: command.Limit})
		if err != nil {
			return nil, err
		}

		return indexSearchResult{
			Project:         current,
			Status:          status,
			ContractVersion: retrieval.ContractVersion,
			Results:         nonNilSlice(results),
		}, nil
	})
}

func runGraph(ctx context.Context, command GraphCommand, commandName, cwd string, output io.Writer) error {
	switch commandName {
	case "graph symbol <name>":
		return runGraphSymbols(ctx, command.Symbol, cwd, output)
	case "graph neighbors <name>":
		return runGraphNeighbors(ctx, command.Neighbors, cwd, output)
	case "graph path <from> <to>":
		return runGraphPaths(ctx, command.Path, cwd, output)
	default:
		return fmt.Errorf("unsupported graph command %q", commandName)
	}
}

func runGraphSymbols(ctx context.Context, command GraphSymbolCommand, cwd string, output io.Writer) error {
	return runIndexApplication(ctx, command.Database, command.JSON, cwd, output, func(indexes *indexingapp.Service, current project.Project) (any, error) {
		symbols, status, err := indexes.Symbols(ctx, current, indexingapp.SymbolRequest{Name: command.Name, Limit: command.Limit})
		if err != nil {
			return nil, err
		}

		return graphSymbolsResult{Project: current, Status: status, ContractVersion: codegraph.ContractVersion, Symbols: nonNilSlice(symbols)}, nil
	})
}

func runGraphNeighbors(ctx context.Context, command GraphNeighborsCommand, cwd string, output io.Writer) error {
	return runIndexApplication(ctx, command.Database, command.JSON, cwd, output, func(indexes *indexingapp.Service, current project.Project) (any, error) {
		neighbors, status, err := indexes.Neighbors(ctx, current, indexingapp.NeighborsRequest{Name: command.Name, Kinds: command.Kinds, Limit: command.Limit})
		if err != nil {
			return nil, err
		}

		return graphNeighborsResult{Project: current, Status: status, ContractVersion: codegraph.ContractVersion, Neighbors: nonNilSlice(neighbors)}, nil
	})
}

func runGraphPaths(ctx context.Context, command GraphPathCommand, cwd string, output io.Writer) error {
	return runIndexApplication(ctx, command.Database, command.JSON, cwd, output, func(indexes *indexingapp.Service, current project.Project) (any, error) {
		paths, status, err := indexes.Paths(ctx, current, indexingapp.PathRequest{From: command.From, To: command.To, MaxDepth: command.MaxDepth, Limit: command.Limit})
		if err != nil {
			return nil, err
		}

		return graphPathsResult{Project: current, Status: status, ContractVersion: codegraph.ContractVersion, Paths: nonNilSlice(paths)}, nil
	})
}

func runIndexApplication(ctx context.Context, database string, jsonOutput bool, cwd string, output io.Writer, action func(*indexingapp.Service, project.Project) (any, error)) error {
	var projects *project.Service
	var indexes *indexingapp.Service
	app := fx.New(
		fx.NopLogger,
		bootstrap.ProjectOptions(database),
		bootstrap.IndexingOptions(os.Getenv("ISTOK_INDEX_ROOT")),
		fx.Invoke(func(projectService *project.Service, indexService *indexingapp.Service) {
			projects = projectService
			indexes = indexService
		}),
	)

	if err := app.Start(ctx); err != nil {
		if jsonOutput {
			return jsonError(err)
		}

		return err
	}

	var value any
	current, err := projects.Current(ctx, cwd)
	if err == nil {
		value, err = action(indexes, current)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop index application: %w", stopErr)
	}

	if jsonOutput && (err != nil || stopErr != nil) {
		return jsonError(errors.Join(err, stopErr))
	}
	if err != nil || stopErr != nil {
		return errors.Join(err, stopErr)
	}

	return writeIndexResult(output, jsonOutput, value)
}

func writeIndexResult(output io.Writer, jsonOutput bool, value any) error {
	if jsonOutput {
		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{SchemaVersion: indexSchemaVersion, Result: value})
	}

	_, err := fmt.Fprintln(output, presentation.ForOutput(output, renderIndexValue(value)))
	return err
}

func renderIndexValue(value any) string {
	switch typed := value.(type) {
	case indexStatusResult:
		return renderIndexStatus(typed.Project, typed.Status)
	case indexSearchResult:
		return renderIndexSearch(typed.Project, typed.Status, typed.Results)
	case graphSymbolsResult:
		return renderGraphSymbols(typed.Project, typed.Status, typed.Symbols)
	case graphNeighborsResult:
		return renderGraphNeighbors(typed.Project, typed.Status, typed.Neighbors)
	case graphPathsResult:
		return renderGraphPaths(typed.Project, typed.Status, typed.Paths)
	default:
		return fmt.Sprint(value)
	}
}

func renderIndexStatus(value project.Project, status indexingapp.Status) string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Index"))
	output.WriteString("\n\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Project"), presentation.Brand(value.Name))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("State"), presentation.StyledStatus(string(status.State)))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Generation"), dash(status.EpochID))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %d", presentation.Key("Revision"), status.Revision)))
	if status.TargetRevision != nil {
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %d", presentation.Key("Target"), *status.TargetRevision)))
	}

	output.WriteString("\n\n")
	output.WriteString(presentation.PrimaryTitle("Diagnostics"))
	output.WriteString("\n")
	if len(status.Diagnostics) == 0 {
		output.WriteString(presentation.Metadata("No diagnostics."))
	} else {
		for _, diagnostic := range status.Diagnostics {
			output.WriteString("\n")
			output.WriteString(presentation.RailLine(presentation.Warning(diagnostic)))
		}
	}

	return output.String()
}

func renderIndexSearch(value project.Project, status indexingapp.Status, results []retrieval.SearchResult) string {
	var output strings.Builder
	output.WriteString(renderIndexStatus(value, status))
	output.WriteString("\n\n")
	output.WriteString(presentation.PrimaryTitle("Search results"))
	if len(results) == 0 {
		output.WriteString("\n")
		output.WriteString(presentation.Metadata("No results."))
		return output.String()
	}

	for _, result := range results {
		output.WriteString("\n\n")
		location := fmt.Sprintf("%s:%d-%d", result.Path, result.LineStart, result.LineEnd)
		output.WriteString(presentation.Brand(location))
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %.3f  %s %s", presentation.Key("Score"), result.Score, presentation.Key("Symbol"), dash(result.Symbol))))
		if len(result.Provenance) > 0 {
			output.WriteString("\n")
			output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Provenance"), strings.Join(result.Provenance, ", "))))
		}
		if result.Snippet != "" {
			output.WriteString("\n")
			output.WriteString(result.Snippet)
		}
	}

	return output.String()
}

func renderGraphSymbols(value project.Project, status indexingapp.Status, symbols []codegraph.Node) string {
	var output strings.Builder
	output.WriteString(renderIndexStatus(value, status))
	output.WriteString("\n\n")
	output.WriteString(presentation.PrimaryTitle("Symbols"))
	if len(symbols) == 0 {
		output.WriteString("\n")
		output.WriteString(presentation.Metadata("No symbols."))
		return output.String()
	}

	rows := make([][]string, 0, len(symbols))
	for _, symbol := range symbols {
		rows = append(rows, []string{symbol.Name, string(symbol.Kind), nodeLocation(symbol)})
	}
	output.WriteString("\n\n")
	output.WriteString(presentation.RenderTable([]string{"SYMBOL", "KIND", "LOCATION"}, rows))
	return output.String()
}

func renderGraphNeighbors(value project.Project, status indexingapp.Status, neighbors []indexingapp.GraphNeighbor) string {
	var output strings.Builder
	output.WriteString(renderIndexStatus(value, status))
	output.WriteString("\n\n")
	output.WriteString(presentation.PrimaryTitle("Neighbors"))
	if len(neighbors) == 0 {
		output.WriteString("\n")
		output.WriteString(presentation.Metadata("No neighbors."))
		return output.String()
	}

	for _, neighbor := range neighbors {
		output.WriteString("\n\n")
		output.WriteString(presentation.Brand(fmt.Sprintf("%s %s %s", neighbor.Source.QualifiedName, neighbor.Kind, neighbor.Target.QualifiedName)))
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Target"), nodeLocation(neighbor.Target))))
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s  %s %.2f", presentation.Key("Provenance"), neighbor.Provenance, presentation.Key("Confidence"), neighbor.Confidence)))
		output.WriteString("\n")
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s:%d", presentation.Key("Evidence"), neighbor.EvidencePath, neighbor.EvidenceLine)))
	}

	return output.String()
}

func renderGraphPaths(value project.Project, status indexingapp.Status, paths []indexingapp.GraphPath) string {
	var output strings.Builder
	output.WriteString(renderIndexStatus(value, status))
	output.WriteString("\n\n")
	output.WriteString(presentation.PrimaryTitle("Paths"))
	if len(paths) == 0 {
		output.WriteString("\n")
		output.WriteString(presentation.Metadata("No paths."))
		return output.String()
	}

	for index, path := range paths {
		output.WriteString("\n\n")
		output.WriteString(presentation.Warning(fmt.Sprintf("Path %d", index+1)))
		for _, node := range path.Nodes {
			output.WriteString("\n")
			output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Brand(dash(node.QualifiedName)), presentation.Path(nodeLocation(node)))))
		}
	}

	return output.String()
}

func nodeLocation(node codegraph.Node) string {
	return fmt.Sprintf("%s:%d-%d", node.Path, node.LineStart, node.LineEnd)
}

func dash(value string) string {
	if value == "" {
		return "-"
	}

	return value
}

func nonNilSlice[T any](values []T) []T {
	if values == nil {
		return []T{}
	}

	return values
}

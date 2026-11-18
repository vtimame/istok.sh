package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	usageapp "github.com/vtimame/istok.sh/internal/application/usage"
	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/usage"
)

// UsageCommand reports how agents used Istok through MCP on this device.
type UsageCommand struct {
	Since    string `name:"since" default:"30d" help:"Report period: days such as 7d, or a duration such as 12h. The log keeps 90 days."`
	Project  string `name:"project" help:"Only calls in this project: ID, unambiguous ID prefix or name. Defaults to every project."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

func runUsage(ctx context.Context, command UsageCommand, output io.Writer) error {
	period, err := parsePeriod(command.Since)
	if err != nil {
		if command.JSON {
			return jsonError(err)
		}

		return err
	}

	service := new(*usageapp.Service)
	app := fx.New(
		fx.NopLogger,
		bootstrap.UsageOptions(command.Database),
		fx.Invoke(func(value *usageapp.Service) { *service = value }),
	)

	return runProjectApplication(ctx, command.JSON, output, app, func() (any, error) {
		return (*service).Report(ctx, usageapp.ReportInput{Period: period, Project: command.Project})
	})
}

// parsePeriod accepts whole days ("30d") or a Go duration ("12h").
func parsePeriod(value string) (time.Duration, error) {
	invalid := &project.Error{Code: project.CodeInvalid, Message: fmt.Sprintf("--since must be days such as 7d or a duration such as 12h, got %q", value)}

	trimmed := strings.TrimSpace(value)
	if days, found := strings.CutSuffix(trimmed, "d"); found {
		count, err := strconv.Atoi(days)
		if err != nil || count <= 0 {
			return 0, invalid
		}

		return time.Duration(count) * 24 * time.Hour, nil
	}

	period, err := time.ParseDuration(trimmed)
	if err != nil || period <= 0 {
		return 0, invalid
	}

	return period, nil
}

func renderUsageReport(report usage.Report) string {
	var output strings.Builder

	output.WriteString(presentation.SectionTitle("Usage"))
	output.WriteString("\n\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Since"), report.Since.Local().Format("2006-01-02 15:04"))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %d  %s %d  %s %d", presentation.Key("Sessions"), report.Sessions, presentation.Key("Calls"), report.Calls, presentation.Key("Failed"), report.Failed)))
	output.WriteString("\n")

	if report.Sessions == 0 {
		output.WriteString("\n")
		output.WriteString(presentation.Metadata("No MCP calls recorded in this period."))

		return output.String()
	}

	if len(report.Clients) > 0 {
		names := make([]string, 0, len(report.Clients))
		for _, client := range report.Clients {
			names = append(names, fmt.Sprintf("%s %d", client.Name, client.Count))
		}
		output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Clients"), strings.Join(names, ", "))))
		output.WriteString("\n")
	}

	funnel := make([][]string, 0, len(report.Funnel))
	for _, step := range report.Funnel {
		funnel = append(funnel, []string{step.Step, strconv.Itoa(step.Sessions), percent(step.Sessions, report.Sessions)})
	}
	output.WriteString("\n")
	output.WriteString(presentation.SectionTitle("Sessions by workflow step"))
	output.WriteString("\n")
	output.WriteString(presentation.RenderTable([]string{"Step", "Sessions", "Share"}, funnel))
	output.WriteString("\n")

	if len(report.Errors) > 0 {
		errors := make([][]string, 0, len(report.Errors))
		for _, value := range report.Errors {
			errors = append(errors, []string{value.Name, strconv.Itoa(value.Count)})
		}
		output.WriteString("\n")
		output.WriteString(presentation.SectionTitle("Errors"))
		output.WriteString("\n")
		output.WriteString(presentation.RenderTable([]string{"Code", "Calls"}, errors))
		output.WriteString("\n")
	}

	tools := make([][]string, 0, len(report.Tools))
	for _, value := range report.Tools {
		tools = append(tools, []string{value.Tool, strconv.Itoa(value.Calls), strconv.Itoa(value.Failed), strconv.Itoa(value.Sessions)})
	}
	output.WriteString("\n")
	output.WriteString(presentation.SectionTitle("Tools"))
	output.WriteString("\n")
	output.WriteString(presentation.RenderTable([]string{"Tool", "Calls", "Failed", "Sessions"}, tools))

	return output.String()
}

func percent(part, total int) string {
	if total == 0 {
		return "0%"
	}

	return fmt.Sprintf("%d%%", part*100/total)
}

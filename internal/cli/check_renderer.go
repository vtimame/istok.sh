package cli

import (
	"fmt"
	"strings"

	"github.com/vtimame/istok.sh/internal/cli/presentation"
	runmodel "github.com/vtimame/istok.sh/internal/run"
)

// writeChecks lists validations one per line: the result, the command, its
// exit code and duration, and who stands behind it. A check Istok ran itself
// is evidence; a check the agent only reported is its word, and says so.
func writeChecks(output *strings.Builder, checks []runmodel.Validation) {
	if len(checks) == 0 {
		output.WriteString(presentation.RailLine("—"))
		output.WriteString("\n")
		return
	}

	rows := make([][]string, 0, len(checks))
	for _, check := range checks {
		rows = append(rows, []string{
			presentation.StyledStatus(string(check.Status)),
			checkCommand(check),
			presentation.Metadata(checkResult(check)),
			checkSource(check),
		})
	}

	for _, line := range strings.Split(strings.TrimSuffix(presentation.RenderRows(rows), "\n"), "\n") {
		output.WriteString(presentation.RailLine(strings.TrimSpace(line)))
		output.WriteString("\n")
	}
}

func checkCommand(check runmodel.Validation) string {
	if command := strings.TrimSpace(check.Command); command != "" {
		return command
	}

	return showText(check.Summary)
}

func checkResult(check runmodel.Validation) string {
	var parts []string
	if check.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit %d", *check.ExitCode))
	}
	if check.DurationMS != nil {
		parts = append(parts, fmt.Sprintf("%d ms", *check.DurationMS))
	}
	if len(parts) == 0 {
		return "—"
	}

	return strings.Join(parts, " · ")
}

func checkSource(check runmodel.Validation) string {
	if check.Source == runmodel.ValidationSourceExecuted {
		return presentation.Brand("run by Istok")
	}

	reporter := strings.TrimSpace(check.Actor.Name)
	if reporter == "" {
		reporter = "agent"
	}

	return presentation.Warning("reported by " + reporter)
}

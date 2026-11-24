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
// Checks in changed ran on code that has changed since.
func writeChecks(output *strings.Builder, checks []runmodel.Validation, changed map[string]bool) {
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
			checkSource(check, changed[check.ID]),
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

func checkSource(check runmodel.Validation, changed bool) string {
	if check.Source == runmodel.ValidationSourceExecuted {
		switch check.TreeState {
		case runmodel.TreeStateBound:
			if changed {
				return presentation.Brand("run by Istok") + presentation.Warning(" · code changed since: run it again")
			}

			return presentation.Brand("run by Istok") + presentation.Metadata(" · tied to code (base "+shortCommit(check.TreeCommit)+")")
		case runmodel.TreeStateNoGit:
			return presentation.Brand("run by Istok") + presentation.Warning(" · not tied to code: no Git")
		case runmodel.TreeStateUnavailable:
			return presentation.Brand("run by Istok") + presentation.Warning(" · not tied to code: Git failed")
		}

		return presentation.Brand("run by Istok")
	}

	reporter := strings.TrimSpace(check.Actor.Name)
	if reporter == "" {
		reporter = "agent"
	}

	return presentation.Warning("reported by " + reporter)
}

// shortCommit abbreviates a commit for display.
func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}

	return commit
}

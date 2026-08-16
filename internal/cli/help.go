package cli

import (
	"fmt"

	"github.com/alecthomas/kong"
)

func taskHelpPrinter(options kong.HelpOptions, ctx *kong.Context) error {
	selected := ctx.Selected()
	if selected == nil || selected.Name != "task" {
		return kong.DefaultHelpPrinter(options, ctx)
	}

	_, err := fmt.Fprint(ctx.Stdout, `Usage: istok task <command>

Inspect and operate tasks in the current project.

Commands:
  create              Create a task in the current project.
  list                List open tasks.
  show ID             Show task details.
  ready               List tasks ready to claim.
  comment ID          Add a comment to a task.
  progress ID         Record task progress.
  dependency          Manage task dependencies.
  claim ID            Claim a ready task and create an active run.
  done ID             Complete a task with validation evidence or an override.

Flags:
  -h, --help  Show help.
`)
	return err
}

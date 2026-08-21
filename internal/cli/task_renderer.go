package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	prettytext "github.com/jedib0t/go-pretty/v6/text"

	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/task"
)

type taskRenderer interface {
	RenderTaskList([]task.TaskListItem) string
	RenderTaskShow(task.Show) string
}

type humanTaskRenderer struct{}

var _ taskRenderer = humanTaskRenderer{}

// RenderTaskList remains available for renderers that do not have project context.
func (humanTaskRenderer) RenderTaskList(values []task.TaskListItem) string {
	return renderTaskList("Tasks", values)
}

func (humanTaskRenderer) RenderProjectTaskList(projectName string, values []task.TaskListItem) string {
	return renderTaskList(projectName, values)
}

func (humanTaskRenderer) RenderProjectReadyTaskList(projectName string, values []task.TaskListItem) string {
	if len(values) == 0 {
		return fmt.Sprintf("%s %s %s\n", presentation.Brand(projectName), presentation.Divider(), presentation.Warning("no ready tasks"))
	}

	return renderTaskListWithSummary(projectName, values, readyTaskCount(len(values)))
}

func (humanTaskRenderer) RenderTaskMutation(projectName string, value task.Task, action string) string {
	var output strings.Builder
	output.WriteString(presentation.Brand(projectName))
	output.WriteString(" ")
	output.WriteString(presentation.Neutral("·"))
	output.WriteString(" ")
	output.WriteString(presentation.StyledNumber(fmt.Sprintf("#%d", value.Number)))
	output.WriteString("\n\n")
	output.WriteString(presentation.PrimaryTitle(value.Title))
	output.WriteString("\n\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Action"), presentation.Metadata(action))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("State"), presentation.StyledStatus(renderEffectiveState(task.DeriveState(value, false, nil))))))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Revision"), presentation.Metadata(fmt.Sprintf("r%d", value.Revision)))))
	output.WriteString("\n")

	return output.String()
}

func renderTaskList(projectName string, values []task.TaskListItem) string {
	return renderTaskListWithSummary(projectName, values, openTaskCount(len(values)))
}

func renderTaskListWithSummary(projectName string, values []task.TaskListItem, summary string) string {
	if len(values) == 0 {
		return fmt.Sprintf("%s %s %s\n", presentation.Brand(projectName), presentation.Divider(), presentation.Warning("no open tasks"))
	}

	hasBlockers := false
	for _, value := range values {
		if len(value.ActiveBlockers) > 0 {
			hasBlockers = true
			break
		}
	}

	headers := []string{"#", "STATE", "TITLE"}
	if hasBlockers {
		headers = append(headers, "BLOCKED BY")
	}

	rows := make([][]string, 0, len(values))
	for _, value := range values {
		row := []string{
			presentation.StyledNumber(fmt.Sprintf("#%d", value.Number)),
			presentation.StyledStatus(taskListState(value)),
			value.Title,
		}
		if hasBlockers {
			row = append(row, taskListBlockedBy(value))
		}
		rows = append(rows, row)
	}

	return fmt.Sprintf("%s %s %s\n\n%s\n", presentation.Brand(projectName), presentation.Divider(), presentation.Warning(summary), presentation.RenderTable(headers, rows))
}

func openTaskCount(count int) string {
	if count == 1 {
		return "1 open task"
	}

	return fmt.Sprintf("%d open tasks", count)
}

func readyTaskCount(count int) string {
	if count == 1 {
		return "1 ready task"
	}

	return fmt.Sprintf("%d ready tasks", count)
}

func taskListState(value task.TaskListItem) string {
	return renderEffectiveState(task.DeriveState(value.Task, value.HasActiveRun && !value.HasExpiredRun, value.ActiveBlockers))
}

func taskListBlockedBy(value task.TaskListItem) string {
	if len(value.ActiveBlockers) == 0 {
		return "—"
	}

	numbers := make([]int64, 0, len(value.ActiveBlockers))
	for _, blocker := range value.ActiveBlockers {
		numbers = append(numbers, blocker.Number)
	}

	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })

	parts := make([]string, 0, len(numbers))
	for _, number := range numbers {
		parts = append(parts, presentation.StyledNumber(fmt.Sprintf("#%d", number)))
	}

	return strings.Join(parts, ", ")
}

// RenderTaskShow remains available for renderers that do not have project context.
func (humanTaskRenderer) RenderTaskShow(value task.Show) string {
	return renderTaskShow("Task", value)
}

func (humanTaskRenderer) RenderProjectTaskShow(projectName string, value task.Show) string {
	return renderTaskShow(projectName, value)
}

func renderTaskShow(projectName string, value task.Show) string {
	var output strings.Builder
	state := renderEffectiveState(task.DeriveState(value.Task, value.HasActiveRun && !value.HasExpiredRun, value.Blockers))

	output.WriteString(presentation.Brand(projectName))
	output.WriteString(" ")
	output.WriteString(presentation.Neutral("·"))
	output.WriteString(" ")
	output.WriteString(presentation.StyledNumber(fmt.Sprintf("#%d", value.Task.Number)))
	output.WriteString("\n\n")
	output.WriteString(presentation.PrimaryTitle(value.Task.Title))
	output.WriteString("\n\n")
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("State"), presentation.StyledStatus(state))))
	output.WriteString("\n")
	revision := presentation.Metadata(fmt.Sprintf("r%d", value.Task.Revision))
	output.WriteString(presentation.RailLine(fmt.Sprintf("%s %s", presentation.Key("Revision"), revision)))
	output.WriteString("\n\n")

	writeTextSection(&output, "Description", value.Task.Description)
	writeTextSection(&output, "Acceptance criteria", value.Task.AcceptanceCriteria)
	writeTextSection(&output, "Notes", value.Task.Notes)

	output.WriteString(presentation.SectionTitle("Relations"))
	output.WriteString("\n")
	writeRelations(&output, "Blocked by", value.Blockers)
	writeRelations(&output, "Blocks", value.Dependents)
	output.WriteString("\n")

	output.WriteString(presentation.SectionTitle("History"))
	output.WriteString("\n")
	writeHistory(&output, value.Events)

	return output.String()
}

func writeTextSection(output *strings.Builder, heading, value string) {
	output.WriteString(presentation.SectionTitle(heading))
	output.WriteString("\n")
	for _, line := range renderTaskRichLines(value, 88) {
		output.WriteString(presentation.RailLine(line))
		output.WriteString("\n")
	}
	output.WriteString("\n")
}

func renderTaskRichLines(value string, width int) []string {
	if strings.TrimSpace(value) == "" {
		return []string{"—"}
	}

	lines := wrapText(value, width)
	for index, line := range lines {
		lines[index] = renderTaskRichLine(line)
	}

	return lines
}

func renderTaskRichLine(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	prefix := line[:len(line)-len(trimmed)]

	switch {
	case strings.HasPrefix(trimmed, "### "):
		return prefix + presentation.SectionTitle(strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")))
	case strings.HasPrefix(trimmed, "## "):
		return prefix + presentation.SectionTitle(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
	case strings.HasPrefix(trimmed, "# "):
		return prefix + presentation.SectionTitle(strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")))
	case strings.HasPrefix(trimmed, "- "):
		return prefix + presentation.Neutral("•") + " " + renderTaskInlineMarkdown(strings.TrimPrefix(trimmed, "- "))
	case strings.HasPrefix(trimmed, "* "):
		return prefix + presentation.Neutral("•") + " " + renderTaskInlineMarkdown(strings.TrimPrefix(trimmed, "* "))
	default:
		return prefix + renderTaskInlineMarkdown(trimmed)
	}
}

func renderTaskInlineMarkdown(value string) string {
	var output strings.Builder
	for len(value) > 0 {
		switch {
		case strings.HasPrefix(value, "`"):
			end := strings.Index(value[1:], "`")
			if end >= 0 {
				output.WriteString(presentation.InlineCode(value[1 : end+1]))
				value = value[end+2:]
				continue
			}
		case strings.HasPrefix(value, "**"):
			end := strings.Index(value[2:], "**")
			if end >= 0 {
				output.WriteString(presentation.Strong(value[2 : end+2]))
				value = value[end+4:]
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(value)
		output.WriteRune(r)
		value = value[size:]
	}

	return output.String()
}

func wrapText(value string, width int) []string {
	var lines []string
	for _, paragraph := range strings.Split(value, "\n") {
		if strings.TrimSpace(paragraph) == "" {
			lines = append(lines, "")
			continue
		}

		lines = append(lines, strings.Split(prettytext.WrapText(paragraph, width), "\n")...)
	}

	return lines
}

func writeRelations(output *strings.Builder, label string, values []task.TaskSummary) {
	items := make([]string, 0, len(values))
	for _, summary := range sortedTaskSummaries(values) {
		items = append(items, fmt.Sprintf("%s · %s · %s", presentation.StyledNumber(fmt.Sprintf("#%d", summary.Number)), presentation.StyledStatus(taskShowSummaryState(summary)), summary.Title))
	}
	if len(items) == 0 {
		items = append(items, "—")
	}

	output.WriteString(presentation.RailLine(presentation.Key(label) + " " + items[0]))
	output.WriteString("\n")
	for _, item := range items[1:] {
		output.WriteString(presentation.RailLine(strings.Repeat(" ", 14) + item))
		output.WriteString("\n")
	}
}

func writeHistory(output *strings.Builder, events []task.Event) {
	if len(events) == 0 {
		output.WriteString(presentation.RailLine("—"))
		output.WriteString("\n")
		return
	}

	hasDetails := false
	for _, event := range events {
		if strings.TrimSpace(event.Body) != "" {
			hasDetails = true
			break
		}
	}

	for index, event := range events {
		if index > 0 {
			output.WriteString("\n")
		}

		row := []string{
			formatHistoryTimestamp(event.CreatedAt),
			presentation.Metadata(event.Type),
			formatActor(event.Actor),
			presentation.Metadata(fmt.Sprintf("r%d", event.TaskRevision)),
		}

		for _, line := range strings.Split(strings.TrimSuffix(presentation.RenderRows([][]string{row}), "\n"), "\n") {
			output.WriteString(presentation.RailLine(strings.TrimSpace(line)))
			output.WriteString("\n")
		}

		if hasDetails {
			for _, line := range renderTaskRichLines(event.Body, 86) {
				output.WriteString(presentation.RailLine("  " + line))
				output.WriteString("\n")
			}
		}
	}
}

func formatHistoryTimestamp(value time.Time) string {
	return value.UTC().Format("2 Jan 2006, 15:04 UTC")
}

func renderEffectiveState(state task.EffectiveState) string {
	return strings.ReplaceAll(string(state), "_", " ")
}

func taskShowSummaryState(summary task.TaskSummary) string {
	if summary.DeletedAt != nil {
		return "deleted"
	}

	return string(summary.Status)
}

func formatActor(actor task.ActorSnapshot) string {
	if name := strings.TrimSpace(actor.Name); name != "" {
		return name
	}
	if kind := strings.TrimSpace(actor.Kind); kind != "" {
		return kind
	}

	return "—"
}

func showText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}

	return value
}

func sortedTaskSummaries(values []task.TaskSummary) []task.TaskSummary {
	result := make([]task.TaskSummary, 0, len(values))
	result = append(result, values...)

	sort.Slice(result, func(i, j int) bool {
		return result[i].Number < result[j].Number
	})

	return result
}

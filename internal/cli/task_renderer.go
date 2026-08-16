package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"s26.dev/istok-cli/internal/cli/presentation"
	"s26.dev/istok-cli/internal/task"
)

type taskRenderer interface {
	RenderTaskList([]task.TaskListItem) string
	RenderTaskShow(task.Show) string
}

type humanTaskRenderer struct{}

var _ taskRenderer = humanTaskRenderer{}

func (humanTaskRenderer) RenderTaskList(values []task.TaskListItem) string {
	if len(values) == 0 {
		return "No open tasks\n"
	}

	rows := make([][]string, 0, len(values))
	for _, value := range values {
		rows = append(rows, taskListRow(value))
	}

	return presentation.RenderTable([]string{"#", "STATE", "TITLE", "BLOCKED BY"}, rows) + "\n"
}

func taskListRow(value task.TaskListItem) []string {
	return []string{
		fmt.Sprint(value.Number),
		taskListState(value),
		value.Title,
		taskListBlockedBy(value),
	}
}

func taskListState(value task.TaskListItem) string {
	return renderEffectiveState(task.DeriveState(value.Task, value.HasActiveRun, value.ActiveBlockers))
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
		parts = append(parts, fmt.Sprintf("#%d", number))
	}

	return strings.Join(parts, ", ")
}

func (humanTaskRenderer) RenderTaskShow(value task.Show) string {
	var output strings.Builder

	output.WriteString(fmt.Sprintf("Task #%d\n", value.Task.Number))
	output.WriteString(fmt.Sprintf("State: %s\n", renderEffectiveState(task.DeriveState(value.Task, value.HasActiveRun, value.Blockers))))
	output.WriteString(fmt.Sprintf("Revision: %d\n", value.Task.Revision))
	output.WriteString(fmt.Sprintf("Title: %s\n\n", showText(value.Task.Title)))

	output.WriteString("Description:\n")
	output.WriteString(showText(value.Task.Description))
	output.WriteString("\n\n")
	output.WriteString("Acceptance criteria:\n")
	output.WriteString(showText(value.Task.AcceptanceCriteria))
	output.WriteString("\n\n")
	output.WriteString("Notes:\n")
	output.WriteString(showText(value.Task.Notes))
	output.WriteString("\n\n")

	output.WriteString("Blockers:\n")
	if len(value.Blockers) == 0 {
		output.WriteString("—\n\n")
	} else {
		blocks := sortedTaskSummaries(value.Blockers)
		rows := make([][]string, 0, len(blocks))
		for _, summary := range blocks {
			rows = append(rows, []string{fmt.Sprintf("#%d", summary.Number), taskShowSummaryState(summary), summary.Title})
		}
		output.WriteString(presentation.RenderTable([]string{"#", "STATE", "TITLE"}, rows))
		output.WriteString("\n\n")
	}

	output.WriteString("Dependents:\n")
	if len(value.Dependents) == 0 {
		output.WriteString("—\n\n")
	} else {
		dependents := sortedTaskSummaries(value.Dependents)
		rows := make([][]string, 0, len(dependents))
		for _, summary := range dependents {
			rows = append(rows, []string{fmt.Sprintf("#%d", summary.Number), taskShowSummaryState(summary), summary.Title})
		}
		output.WriteString(presentation.RenderTable([]string{"#", "STATE", "TITLE"}, rows))
		output.WriteString("\n\n")
	}

	output.WriteString("Event history:\n")
	if len(value.Events) == 0 {
		output.WriteString("—\n")
		return output.String()
	}

	rows := make([][]string, 0, len(value.Events))
	for _, event := range value.Events {
		rows = append(rows, []string{event.CreatedAt.UTC().Format(time.RFC3339), event.Type, fmt.Sprint(event.TaskRevision), formatActor(event.Actor), showText(event.Body)})
	}
	output.WriteString(presentation.RenderTable([]string{"WHEN", "EVENT", "REVISION", "ACTOR", "DETAILS"}, rows))
	output.WriteString("\n")

	return output.String()
}

func renderEffectiveState(state task.EffectiveState) string {
	return strings.ToUpper(strings.ReplaceAll(string(state), "_", " "))
}

func taskShowSummaryState(summary task.TaskSummary) string {
	if summary.DeletedAt != nil {
		return "DELETED"
	}

	return strings.ToUpper(string(summary.Status))
}

func formatActor(actor task.ActorSnapshot) string {
	name := strings.TrimSpace(actor.Name)
	kind := strings.TrimSpace(actor.Kind)
	if name == "" && kind == "" {
		return "—"
	}
	if kind == "" {
		return name
	}
	if name == "" {
		return fmt.Sprintf("(%s)", kind)
	}

	return fmt.Sprintf("%s (%s)", name, kind)
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

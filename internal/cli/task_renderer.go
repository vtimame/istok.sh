package cli

import (
	"fmt"
	"sort"
	"strings"

	"s26.dev/istok-cli/internal/cli/presentation"
	"s26.dev/istok-cli/internal/task"
)

type taskListRenderer interface {
	RenderTaskList([]task.TaskListItem) string
}

type taskListTableRenderer struct{}

func (r taskListTableRenderer) RenderTaskList(values []task.TaskListItem) string {
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
	if value.HasActiveRun {
		return "IN PROGRESS"
	}
	if value.Status == task.StatusBlocked || len(value.ActiveBlockers) > 0 {
		return "BLOCKED"
	}

	return "READY"
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

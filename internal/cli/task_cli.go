package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/fx"

	"s26.dev/istok-cli/internal/application/bootstrap"
	taskapp "s26.dev/istok-cli/internal/application/task"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/task"
)

const taskSchemaVersion = "1"

var cliTaskActor = task.ActorSnapshot{ID: "cli", Kind: "cli", Name: "CLI"}

type taskCommandResultKind int

const (
	taskCommandResultTask taskCommandResultKind = iota
	taskCommandResultList
	taskCommandResultShow
)

type taskCommandResult struct {
	kind    taskCommandResultKind
	Project project.Project
	Task    task.Task
	Tasks   []task.TaskListItem
	Show    task.Show
	Action  string
}

type taskListItemJSON struct {
	taskValueJSON
	HasActiveRun   bool              `json:"has_active_run"`
	ActiveBlockers []taskSummaryJSON `json:"active_blockers"`
}

type taskShowJSON struct {
	Task         taskValueJSON        `json:"task"`
	Events       []taskEventJSON      `json:"events"`
	Incoming     []taskDependencyJSON `json:"incoming"`
	Outgoing     []taskDependencyJSON `json:"outgoing"`
	HasActiveRun bool                 `json:"has_active_run"`
	Blockers     []taskSummaryJSON    `json:"blockers"`
	Dependents   []taskSummaryJSON    `json:"dependents"`
}

func (r taskCommandResult) MarshalJSON() ([]byte, error) {
	switch r.kind {
	case taskCommandResultTask:
		return json.Marshal(struct {
			Project projectJSON   `json:"project"`
			Task    taskValueJSON `json:"task"`
		}{Project: projectJSONValue(r.Project), Task: taskJSONValue(r.Task)})
	case taskCommandResultList:
		return json.Marshal(struct {
			Project projectJSON        `json:"project"`
			Tasks   []taskListItemJSON `json:"tasks"`
		}{Project: projectJSONValue(r.Project), Tasks: taskListJSON(r.Tasks)})
	case taskCommandResultShow:
		return json.Marshal(struct {
			Project projectJSON  `json:"project"`
			Task    taskShowJSON `json:"task"`
		}{Project: projectJSONValue(r.Project), Task: taskShowJSONValue(r.Show)})
	default:
		return nil, fmt.Errorf("unsupported task command result")
	}
}

func runTaskList(ctx context.Context, database, cwd string, output io.Writer) error {
	return runTaskListCommand(ctx, TaskListCommand{Database: database}, cwd, output)
}

func runTaskListCommand(ctx context.Context, command TaskListCommand, cwd string, output io.Writer) error {
	return runTaskApplication(ctx, command.Database, command.JSON, output, func(projects *project.Service, tasks *taskapp.Service) (taskCommandResult, error) {
		current, err := projects.Current(ctx, cwd)
		if err != nil {
			return taskCommandResult{}, err
		}

		statuses := command.Status
		if len(statuses) == 0 {
			statuses = []task.Status{task.StatusOpen, task.StatusBlocked}
		}

		values, err := tasks.List(ctx, current.ID, task.ListOptions{Statuses: statuses})
		if err != nil {
			return taskCommandResult{}, err
		}

		return taskCommandResult{kind: taskCommandResultList, Project: current, Tasks: values}, nil
	})
}

func runTaskShow(ctx context.Context, command TaskShowCommand, cwd string, output io.Writer) error {
	return runTaskApplication(ctx, command.Database, command.JSON, output, func(projects *project.Service, tasks *taskapp.Service) (taskCommandResult, error) {
		current, err := projects.Current(ctx, cwd)
		if err != nil {
			return taskCommandResult{}, err
		}

		value, err := tasks.Show(ctx, task.Selector{ProjectID: current.ID, Number: command.ID}, false)
		if err != nil {
			return taskCommandResult{}, err
		}

		return taskCommandResult{kind: taskCommandResultShow, Project: current, Show: value}, nil
	})
}

func runTaskCommand(ctx context.Context, command TaskCommand, commandName, cwd string, output io.Writer) error {
	switch {
	case strings.HasPrefix(commandName, "task create"):
		return runTaskCreate(ctx, command.Create, cwd, output)
	case commandName == "task list":
		return runTaskListCommand(ctx, command.List, cwd, output)
	case strings.HasPrefix(commandName, "task show"):
		return runTaskShow(ctx, command.Show, cwd, output)
	case commandName == "task ready":
		return runTaskReady(ctx, command.Ready, cwd, output)
	case strings.HasPrefix(commandName, "task comment"):
		return runTaskText(ctx, command.Comment.ID, command.Comment.Body, command.Comment.Database, command.Comment.JSON, cwd, output, "comment", (*taskapp.Service).Comment)
	case strings.HasPrefix(commandName, "task progress"):
		return runTaskText(ctx, command.Progress.ID, command.Progress.Body, command.Progress.Database, command.Progress.JSON, cwd, output, "progress", (*taskapp.Service).Progress)
	case strings.HasPrefix(commandName, "task dependency add"):
		return runTaskDependency(ctx, command.Dependency.Add, true, cwd, output)
	case strings.HasPrefix(commandName, "task dependency remove"):
		return runTaskDependency(ctx, TaskDependencyAddCommand(command.Dependency.Remove), false, cwd, output)
	default:
		return fmt.Errorf("unsupported task command %q", commandName)
	}
}

func runTaskCreate(ctx context.Context, command TaskCreateCommand, cwd string, output io.Writer) error {
	return runTaskApplication(ctx, command.Database, command.JSON, output, func(projects *project.Service, tasks *taskapp.Service) (taskCommandResult, error) {
		current, err := projects.Current(ctx, cwd)
		if err != nil {
			return taskCommandResult{}, err
		}

		value, err := tasks.Create(ctx, task.CreateInput{
			ProjectID:          current.ID,
			Title:              command.Title,
			Description:        command.Description,
			AcceptanceCriteria: command.AcceptanceCriteria,
			Notes:              command.Notes,
		}, cliTaskActor)
		if err != nil {
			return taskCommandResult{}, err
		}

		return taskCommandResult{kind: taskCommandResultTask, Project: current, Task: value, Action: "Created"}, nil
	})
}

func runTaskReady(ctx context.Context, command TaskReadyCommand, cwd string, output io.Writer) error {
	return runTaskApplication(ctx, command.Database, command.JSON, output, func(projects *project.Service, tasks *taskapp.Service) (taskCommandResult, error) {
		current, err := projects.Current(ctx, cwd)
		if err != nil {
			return taskCommandResult{}, err
		}

		values, err := tasks.Ready(ctx, current.ID)
		if err != nil {
			return taskCommandResult{}, err
		}

		return taskCommandResult{kind: taskCommandResultList, Project: current, Tasks: values, Action: "ready"}, nil
	})
}

func runTaskText(
	ctx context.Context,
	number int64,
	body, database string,
	jsonOutput bool,
	cwd string,
	output io.Writer,
	action string,
	mutate func(*taskapp.Service, context.Context, task.Selector, int64, string, task.ActorSnapshot) (task.Task, error),
) error {
	return runTaskApplication(ctx, database, jsonOutput, output, func(projects *project.Service, tasks *taskapp.Service) (taskCommandResult, error) {
		current, err := projects.Current(ctx, cwd)
		if err != nil {
			return taskCommandResult{}, err
		}

		shown, err := tasks.Show(ctx, task.Selector{ProjectID: current.ID, Number: number}, false)
		if err != nil {
			return taskCommandResult{}, err
		}

		value, err := mutate(tasks, ctx, task.Selector{ProjectID: current.ID, Number: number}, shown.Task.Revision, body, cliTaskActor)
		if err != nil {
			return taskCommandResult{}, err
		}

		return taskCommandResult{kind: taskCommandResultTask, Project: current, Task: value, Action: action}, nil
	})
}

func runTaskDependency(ctx context.Context, command TaskDependencyAddCommand, add bool, cwd string, output io.Writer) error {
	return runTaskApplication(ctx, command.Database, command.JSON, output, func(projects *project.Service, tasks *taskapp.Service) (taskCommandResult, error) {
		current, err := projects.Current(ctx, cwd)
		if err != nil {
			return taskCommandResult{}, err
		}

		blocked, err := tasks.Show(ctx, task.Selector{ProjectID: current.ID, Number: command.ID}, false)
		if err != nil {
			return taskCommandResult{}, err
		}

		blocker := task.Selector{ProjectID: current.ID, Number: command.Blocker}
		blockedSelector := task.Selector{ProjectID: current.ID, Number: command.ID}
		var value task.Task
		if add {
			value, err = tasks.AddDependency(ctx, blocker, blockedSelector, blocked.Task.Revision, cliTaskActor)
		} else {
			value, err = tasks.RemoveDependency(ctx, blocker, blockedSelector, blocked.Task.Revision, cliTaskActor)
		}
		if err != nil {
			return taskCommandResult{}, err
		}

		action := "Dependency added"
		if !add {
			action = "Dependency removed"
		}
		return taskCommandResult{kind: taskCommandResultTask, Project: current, Task: value, Action: action}, nil
	})
}

func runTaskApplication(ctx context.Context, database string, jsonOutput bool, output io.Writer, action func(*project.Service, *taskapp.Service) (taskCommandResult, error)) error {
	var projects *project.Service
	var tasks *taskapp.Service
	app := fx.New(
		fx.NopLogger,
		bootstrap.TaskOptions(database),
		fx.Invoke(func(projectService *project.Service, taskService *taskapp.Service) {
			projects = projectService
			tasks = taskService
		}),
	)
	if err := app.Start(ctx); err != nil {
		if jsonOutput {
			return jsonTaskError(err)
		}
		return err
	}

	value, actionErr := action(projects, tasks)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop task application: %w", stopErr)
	}
	if err := errors.Join(actionErr, stopErr); err != nil {
		if jsonOutput {
			return jsonTaskError(err)
		}
		return err
	}

	if jsonOutput {
		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string            `json:"schema_version"`
			Result        taskCommandResult `json:"result"`
		}{SchemaVersion: taskSchemaVersion, Result: value})
	}

	_, err := fmt.Fprint(output, renderTaskCommandResult(value))
	return err
}

func jsonTaskError(err error) error {
	encoded, marshalErr := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code    task.Code `json:"code"`
			Message string    `json:"message"`
		} `json:"error"`
	}{SchemaVersion: taskSchemaVersion, Error: struct {
		Code    task.Code `json:"code"`
		Message string    `json:"message"`
	}{Code: taskErrorCode(err), Message: err.Error()}})
	if marshalErr != nil {
		return err
	}

	return jsonCommandError{value: string(encoded), cause: err}
}

func taskErrorCode(err error) task.Code {
	var projectError *project.Error
	if errors.As(err, &projectError) {
		return task.Code(projectError.Code)
	}

	return task.ErrorCode(err)
}

func renderTaskCommandResult(value taskCommandResult) string {
	switch value.kind {
	case taskCommandResultTask:
		return humanTaskRenderer{}.RenderTaskMutation(value.Project.Name, value.Task, value.Action)
	case taskCommandResultList:
		if value.Action == "ready" {
			return humanTaskRenderer{}.RenderProjectReadyTaskList(value.Project.Name, value.Tasks)
		}
		return humanTaskRenderer{}.RenderProjectTaskList(value.Project.Name, value.Tasks)
	case taskCommandResultShow:
		return humanTaskRenderer{}.RenderProjectTaskShow(value.Project.Name, value.Show)
	default:
		return ""
	}
}

func taskListJSON(values []task.TaskListItem) []taskListItemJSON {
	if len(values) == 0 {
		return []taskListItemJSON{}
	}

	result := make([]taskListItemJSON, 0, len(values))
	for _, value := range values {
		result = append(result, taskListItemJSON{
			taskValueJSON:  taskJSONValue(value.Task),
			HasActiveRun:   value.HasActiveRun,
			ActiveBlockers: taskSummariesJSON(value.ActiveBlockers),
		})
	}

	return result
}

func taskShowJSONValue(value task.Show) taskShowJSON {
	return taskShowJSON{
		Task:         taskJSONValue(value.Task),
		Events:       taskEventsJSON(value.Events),
		Incoming:     taskDependenciesJSON(value.Incoming),
		Outgoing:     taskDependenciesJSON(value.Outgoing),
		HasActiveRun: value.HasActiveRun,
		Blockers:     taskSummariesJSON(value.Blockers),
		Dependents:   taskSummariesJSON(value.Dependents),
	}
}

func taskEventsJSON(values []task.Event) []taskEventJSON {
	result := make([]taskEventJSON, 0, len(values))
	for _, value := range values {
		result = append(result, taskEventJSON{
			ID: value.ID, TaskID: value.TaskID, Type: value.Type, Body: value.Body,
			TaskRevision: value.TaskRevision, Actor: taskActorJSONValue(value.Actor), CreatedAt: value.CreatedAt,
		})
	}

	return result
}

func taskDependenciesJSON(values []task.Dependency) []taskDependencyJSON {
	result := make([]taskDependencyJSON, 0, len(values))
	for _, value := range values {
		result = append(result, taskDependencyJSON{
			ID: value.ID, ProjectID: value.ProjectID, BlockerTaskID: value.BlockerTaskID,
			BlockedTaskID: value.BlockedTaskID, EdgeType: value.EdgeType, CreatedAt: value.CreatedAt,
		})
	}

	return result
}

func taskSummariesJSON(values []task.TaskSummary) []taskSummaryJSON {
	result := make([]taskSummaryJSON, 0, len(values))
	for _, value := range values {
		result = append(result, taskSummaryJSON{
			ID: value.ID, Number: value.Number, Status: string(value.Status), Title: value.Title, DeletedAt: value.DeletedAt,
		})
	}

	return result
}

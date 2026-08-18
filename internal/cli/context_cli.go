package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/fx"

	"s26.dev/istok-cli/internal/application/bootstrap"
	contextapp "s26.dev/istok-cli/internal/application/context"
	contextmodel "s26.dev/istok-cli/internal/context"
	"s26.dev/istok-cli/internal/project"
)

var cliContextActor = contextmodel.ActorSnapshot{ID: "cli", Kind: "cli", Name: "CLI"}

type contextView struct {
	Project project.Project                   `json:"project"`
	Record  contextmodel.ProjectContextRecord `json:"record"`
	Events  []contextmodel.ContextEvent       `json:"events,omitempty"`
}

type contextMarkdownView struct {
	Project project.Project                     `json:"project"`
	Records []contextmodel.ProjectContextRecord `json:"records"`
}

type contextListView struct {
	Project project.Project                     `json:"project"`
	Records []contextmodel.ProjectContextRecord `json:"records"`
}

func runContext(ctx context.Context, command ContextCommand, commandName, cwd string, input io.Reader, output io.Writer) error {
	switch {
	case strings.HasPrefix(commandName, "context add"):
		return runContextApp(ctx, command.Add.Database, command.Add.JSON, output, func(projects *project.Service, contexts *contextapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}

			value, err := contexts.Create(ctx, contextmodel.CreateInput{
				ProjectID:   current.ID,
				Kind:        command.Add.Kind,
				Title:       command.Add.Title,
				Body:        command.Add.Body,
				Tags:        command.Add.Tag,
				Source:      command.Add.Source,
				Visibility:  command.Add.Visibility,
				Sensitivity: command.Add.Sensitivity,
				Priority:    command.Add.Priority,
				Scope:       command.Add.Scope,
			}, cliContextActor)
			if err != nil {
				return nil, err
			}

			return contextView{Project: current, Record: value}, nil
		})
	case commandName == "context list":
		return runContextApp(ctx, command.List.Database, command.List.JSON, output, func(projects *project.Service, contexts *contextapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}

			values, err := contexts.List(ctx, current.ID, contextmodel.ListOptions{
				Kinds:           command.List.Kind,
				Sources:         command.List.Source,
				Visibilities:    command.List.Visibility,
				Sensitivities:   command.List.Sensitivity,
				IncludeDeleted:  command.List.IncludeDeleted,
				IncludeDisabled: command.List.IncludeDisabled,
				Limit:           command.List.Limit,
			})
			if err != nil {
				return nil, err
			}

			return contextListView{Project: current, Records: values}, nil
		})
	case strings.HasPrefix(commandName, "context show"):
		return runContextApp(ctx, command.Show.Database, command.Show.JSON, output, func(projects *project.Service, contexts *contextapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(command.Show.ID) == "" {
				values, err := contexts.List(ctx, current.ID, contextmodel.ListOptions{
					IncludeDeleted:  command.Show.IncludeDeleted,
					IncludeDisabled: command.Show.IncludeDisabled,
					Limit:           0,
				})
				if err != nil {
					return nil, err
				}

				return contextMarkdownView{Project: current, Records: values}, nil
			}

			value, err := contexts.Get(ctx, command.Show.ID, command.Show.IncludeDeleted)
			if err != nil {
				return nil, err
			}
			if value.ProjectID != current.ID {
				return nil, contextmodel.NewError(contextmodel.CodeNotFound, "context record was not found")
			}

			events, err := contexts.Events(ctx, value.ID)
			if err != nil {
				return nil, err
			}

			return contextView{Project: current, Record: value, Events: events}, nil
		})
	case strings.HasPrefix(commandName, "context search"):
		return runContextApp(ctx, command.Search.Database, command.Search.JSON, output, func(projects *project.Service, contexts *contextapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}

			values, err := contexts.Search(ctx, current.ID, contextmodel.SearchOptions{
				Query:           command.Search.Query,
				IncludeDeleted:  command.Search.IncludeDeleted,
				IncludeDisabled: command.Search.IncludeDisabled,
				Limit:           command.Search.Limit,
			})
			if err != nil {
				return nil, err
			}

			return contextListView{Project: current, Records: values}, nil
		})
	case strings.HasPrefix(commandName, "context update"):
		return runContextApp(ctx, command.Update.Database, command.Update.JSON, output, func(projects *project.Service, contexts *contextapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			existing, err := contexts.Get(ctx, command.Update.ID, false)
			if err != nil {
				return nil, err
			}
			if existing.ProjectID != current.ID {
				return nil, contextmodel.NewError(contextmodel.CodeNotFound, "context record was not found")
			}

			patch := contextmodel.Patch{
				Kind:        command.Update.Kind,
				Title:       command.Update.Title,
				Body:        command.Update.Body,
				Source:      command.Update.Source,
				Visibility:  command.Update.Visibility,
				Sensitivity: command.Update.Sensitivity,
				Priority:    command.Update.Priority,
				Scope:       command.Update.Scope,
			}
			if len(command.Update.Tag) > 0 {
				patch.Tags = &command.Update.Tag
			}

			value, err := contexts.Update(ctx, command.Update.ID, command.Update.ExpectedRevision, patch, cliContextActor)
			if err != nil {
				return nil, err
			}

			return contextView{Project: current, Record: value}, nil
		})
	case strings.HasPrefix(commandName, "context delete"):
		if !command.Delete.Yes {
			if command.Delete.JSON {
				return jsonContextError(contextmodel.NewError(contextmodel.CodeInvalid, "context deletion requires --yes"))
			}

			fmt.Fprint(output, "Archive context record? [y/N] ")
			answer, err := bufio.NewReader(input).ReadString('\n')
			if err != nil && len(answer) == 0 {
				return fmt.Errorf("read context deletion confirmation: %w", err)
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				_, err := fmt.Fprintln(output, "Context deletion cancelled.")
				return err
			}
		}

		return runContextApp(ctx, command.Delete.Database, command.Delete.JSON, output, func(projects *project.Service, contexts *contextapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			existing, err := contexts.Get(ctx, command.Delete.ID, false)
			if err != nil {
				return nil, err
			}
			if existing.ProjectID != current.ID {
				return nil, contextmodel.NewError(contextmodel.CodeNotFound, "context record was not found")
			}

			value, err := contexts.Archive(ctx, command.Delete.ID, command.Delete.ExpectedRevision, cliContextActor)
			if err != nil {
				return nil, err
			}

			return contextView{Project: current, Record: value}, nil
		})
	case strings.HasPrefix(commandName, "context enable"), strings.HasPrefix(commandName, "context disable"):
		input := command.Enable
		enabled := strings.HasPrefix(commandName, "context enable")
		if !enabled {
			input = command.Disable
		}
		return runContextApp(ctx, input.Database, input.JSON, output, func(projects *project.Service, contexts *contextapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			record, err := contexts.Get(ctx, input.ID, false)
			if err != nil {
				return nil, err
			}
			if record.ProjectID != current.ID {
				return nil, contextmodel.NewError(contextmodel.CodeNotFound, "context record was not found")
			}
			value, err := contexts.Update(ctx, record.ID, input.ExpectedRevision, contextmodel.Patch{Enabled: &enabled}, cliContextActor)
			if err != nil {
				return nil, err
			}
			return contextView{Project: current, Record: value}, nil
		})
	default:
		return fmt.Errorf("unsupported command %q", commandName)
	}
}

func runContextApp(ctx context.Context, database string, jsonOutput bool, output io.Writer, action func(*project.Service, *contextapp.Service) (any, error)) error {
	var projects *project.Service
	var contexts *contextapp.Service

	app := fx.New(
		fx.NopLogger,
		bootstrap.ContextOptions(database),
		fx.Invoke(func(projectService *project.Service, contextService *contextapp.Service) {
			projects = projectService
			contexts = contextService
		}),
	)
	if err := app.Start(ctx); err != nil {
		if jsonOutput {
			return jsonContextError(err)
		}

		return err
	}

	value, err := action(projects, contexts)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop context application: %w", stopErr)
	}

	if jsonOutput {
		if err != nil || stopErr != nil {
			return jsonContextError(errors.Join(err, stopErr))
		}

		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{"1", value})
	}
	if err != nil {
		return errors.Join(err, stopErr)
	}

	_, err = fmt.Fprint(output, renderContextValue(value))
	return errors.Join(err, stopErr)
}

func jsonContextError(err error) error {
	encoded, marshalErr := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{SchemaVersion: "1", Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: contextErrorCode(err), Message: err.Error()}})
	if marshalErr != nil {
		return err
	}

	return jsonCommandError{value: string(encoded), cause: err}
}

func contextErrorCode(err error) string {
	var contextError *contextmodel.Error
	if errors.As(err, &contextError) {
		return string(contextError.Code)
	}

	var projectError *project.Error
	if errors.As(err, &projectError) {
		return string(projectError.Code)
	}

	return string(contextmodel.CodeInternal)
}

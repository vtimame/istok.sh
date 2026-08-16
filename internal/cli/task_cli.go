package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"go.uber.org/fx"

	"s26.dev/istok-cli/internal/application/bootstrap"
	taskapp "s26.dev/istok-cli/internal/application/task"
	"s26.dev/istok-cli/internal/project"
)

func runTaskList(ctx context.Context, database, cwd string, output io.Writer) error {
	return runTaskRead(ctx, database, output, func(resolver taskReadAPI) (string, error) {
		read, err := resolver.List(ctx, cwd)
		if err != nil {
			return "", err
		}

		return humanTaskRenderer{}.RenderTaskList(read), nil
	})
}

func runTaskShow(ctx context.Context, command TaskShowCommand, cwd string, output io.Writer) error {
	return runTaskRead(ctx, command.Database, output, func(resolver taskReadAPI) (string, error) {
		read, err := resolver.Show(ctx, cwd, command)
		if err != nil {
			return "", err
		}

		return humanTaskRenderer{}.RenderTaskShow(read), nil
	})
}

func runTaskRead(ctx context.Context, database string, output io.Writer, run func(taskReadAPI) (string, error)) error {
	var resolver taskReadAPI

	app := fx.New(
		fx.NopLogger,
		bootstrap.TaskOptions(database),
		fx.Invoke(func(projects *project.Service, tasks *taskapp.Service) {
			resolver = newTaskReadResolver(projects, tasks)
		}),
	)

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start task application: %w", err)
	}

	rendered, runErr := run(resolver)

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop task application: %w", stopErr)
	}
	if runErr != nil || stopErr != nil {
		return errors.Join(runErr, stopErr)
	}

	_, writeErr := fmt.Fprint(output, rendered)
	return writeErr
}

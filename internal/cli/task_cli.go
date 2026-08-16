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

	read, err := resolver.List(ctx, cwd)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopErr := app.Stop(stopCtx)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop task application: %w", stopErr)
	}
	if err != nil || stopErr != nil {
		return errors.Join(err, stopErr)
	}

	table := taskListTableRenderer{}.RenderTaskList(read)
	_, writeErr := fmt.Fprint(output, table)
	return errors.Join(writeErr, stopErr)
}

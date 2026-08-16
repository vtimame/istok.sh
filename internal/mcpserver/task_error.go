package mcpserver

import (
	"errors"

	contextmodel "s26.dev/istok-cli/internal/context"
	"s26.dev/istok-cli/internal/project"
	run "s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

func errorCode(err error) string {
	var contextError *contextmodel.Error
	if errors.As(err, &contextError) {
		return string(contextError.Code)
	}

	var taskError *task.Error
	if errors.As(err, &taskError) {
		return string(taskError.Code)
	}

	var projectError *project.Error
	if errors.As(err, &projectError) {
		return string(projectError.Code)
	}

	var runError *run.Error
	if errors.As(err, &runError) {
		return string(runError.Code)
	}

	return string(task.CodeInternal)
}

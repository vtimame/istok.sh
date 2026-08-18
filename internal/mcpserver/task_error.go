package mcpserver

import (
	"errors"

	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/project"
	run "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
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
	if indexingapp.IsError(err) {
		return indexingapp.ErrorCode
	}

	return string(task.CodeInternal)
}

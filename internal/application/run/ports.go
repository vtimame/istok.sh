package runapp

import (
	"context"

	contextpack "github.com/vtimame/istok.sh/internal/contextpack"
	run "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type TaskResolver interface {
	Resolve(context.Context, task.Selector, bool) (task.Task, error)
}

type ContextPackageBuilder interface {
	BuildForTask(context.Context, task.Task, contextpack.BuildOptions) (contextpack.Package, error)
}

type Repository interface {
	Claim(context.Context, run.ClaimRecord) (run.Run, error)
	GetRun(context.Context, string) (run.Run, error)
	ListRuns(context.Context, run.ListOptions) ([]run.Run, error)
	ShowRun(context.Context, string) (run.Show, error)
	HasActiveRun(context.Context, string) (bool, error)
	TaskRunState(context.Context, string) (run.TaskRunState, error)
	StartExecution(context.Context, run.StartExecutionInput, run.ActorSnapshot) (run.Execution, error)
	FinishExecution(context.Context, run.FinishExecutionInput, run.ActorSnapshot) (run.Execution, error)
	FinishManagedExecution(context.Context, run.FinishManagedExecutionInput, run.ActorSnapshot) (run.FinishManagedExecutionResult, error)
	Heartbeat(context.Context, run.HeartbeatInput, run.ActorSnapshot) (run.Run, error)
	Recover(context.Context, run.RecoverInput, run.ActorSnapshot) (run.Run, error)
	Abandon(context.Context, run.AbandonInput, run.ActorSnapshot) (run.Run, error)
	GetArtifact(context.Context, string) (run.Artifact, error)
	ListArtifacts(context.Context, string) ([]run.Artifact, error)
	RecordValidation(context.Context, run.RecordValidationInput, run.ActorSnapshot) (run.Validation, error)
	FinishRun(context.Context, run.FinishRunInput, run.ActorSnapshot) (run.Run, error)
	CompleteTask(context.Context, run.CompletionRecord) (task.Task, run.Completion, error)
}

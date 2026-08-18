package contextapp

import (
	"context"
	"time"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
)

type Repository interface {
	Create(context.Context, contextmodel.CreateInput, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error)
	Get(context.Context, string, bool) (contextmodel.ProjectContextRecord, error)
	Update(context.Context, string, int64, contextmodel.Patch, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error)
	Archive(context.Context, string, int64, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error)
	Restore(context.Context, string, int64, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error)
	List(context.Context, string, contextmodel.ListOptions) ([]contextmodel.ProjectContextRecord, error)
	Search(context.Context, string, contextmodel.SearchOptions) ([]contextmodel.ProjectContextRecord, error)
	Events(context.Context, string) ([]contextmodel.ContextEvent, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

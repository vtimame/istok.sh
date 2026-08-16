package contextapp

import (
	"context"

	contextmodel "s26.dev/istok-cli/internal/context"
)

func (s *Service) Update(ctx context.Context, id string, expected int64, patch contextmodel.Patch, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if err := requireMutation(expected, actor); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	if err := patch.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	if !contextmodel.IsUUIDv7(id) {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id must be a canonical UUIDv7")
	}

	return s.repository.Update(ctx, id, expected, patch, actor)
}

func (s *Service) Archive(ctx context.Context, id string, expected int64, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if err := requireMutation(expected, actor); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	if !contextmodel.IsUUIDv7(id) {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id must be a canonical UUIDv7")
	}

	return s.repository.Archive(ctx, id, expected, actor)
}

func (s *Service) Restore(ctx context.Context, id string, expected int64, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if err := requireMutation(expected, actor); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	if !contextmodel.IsUUIDv7(id) {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id must be a canonical UUIDv7")
	}

	return s.repository.Restore(ctx, id, expected, actor)
}

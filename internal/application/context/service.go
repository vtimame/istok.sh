package contextapp

import (
	"context"
	"time"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
)

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func NewServiceWithClock(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (s *Service) Create(ctx context.Context, input contextmodel.CreateInput, actor contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	if err := input.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}
	if err := actor.Validate(); err != nil {
		return contextmodel.ProjectContextRecord{}, err
	}

	return s.repository.Create(ctx, input, actor)
}

func (s *Service) Get(ctx context.Context, id string, includeDeleted bool) (contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(id) {
		return contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeInvalid, "context record id must be a canonical UUIDv7")
	}

	return s.repository.Get(ctx, id, includeDeleted)
}

func (s *Service) List(ctx context.Context, projectID string, options contextmodel.ListOptions) ([]contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(projectID) {
		return nil, contextmodel.NewError(contextmodel.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	return s.repository.List(ctx, projectID, options)
}

func (s *Service) Search(ctx context.Context, projectID string, options contextmodel.SearchOptions) ([]contextmodel.ProjectContextRecord, error) {
	if !contextmodel.IsUUIDv7(projectID) {
		return nil, contextmodel.NewError(contextmodel.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	return s.repository.Search(ctx, projectID, options)
}

func (s *Service) Doctor(ctx context.Context, projectID string) (contextmodel.DiagnosticReport, error) {
	values, err := s.List(ctx, projectID, contextmodel.ListOptions{IncludeDisabled: true})
	if err != nil {
		return contextmodel.DiagnosticReport{}, err
	}

	return contextmodel.Diagnose(values, s.now().UTC()), nil
}

func (s *Service) Events(ctx context.Context, recordID string) ([]contextmodel.ContextEvent, error) {
	if !contextmodel.IsUUIDv7(recordID) {
		return nil, contextmodel.NewError(contextmodel.CodeInvalid, "context record id must be a canonical UUIDv7")
	}

	return s.repository.Events(ctx, recordID)
}

func requireMutation(expected int64, actor contextmodel.ActorSnapshot) error {
	if expected < 1 {
		return contextmodel.NewError(contextmodel.CodeInvalid, "expected revision is required")
	}

	return actor.Validate()
}

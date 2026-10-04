package knowledgeapp

import (
	"context"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
)

type Repository interface {
	Create(context.Context, knowledge.CreateInput, contextmodel.ActorSnapshot) (knowledge.Item, error)
	Get(context.Context, string) (knowledge.Item, error)
	Update(context.Context, string, int64, knowledge.Patch, contextmodel.ActorSnapshot) (knowledge.Item, error)
	Review(context.Context, string, int64, string, contextmodel.ActorSnapshot) (knowledge.Item, error)
	Promote(context.Context, string, int64, contextmodel.ActorSnapshot) (knowledge.Item, error)
	Supersede(context.Context, string, int64, string, contextmodel.ActorSnapshot) (knowledge.Item, knowledge.Item, error)
	Catalog(context.Context, string, knowledge.CatalogOptions) ([]knowledge.CatalogItem, error)
	Search(context.Context, string, knowledge.SearchOptions) ([]knowledge.CatalogItem, error)
	Events(context.Context, string) ([]knowledge.Event, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Create(ctx context.Context, input knowledge.CreateInput, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	if err := input.Validate(); err != nil {
		return knowledge.Item{}, err
	}
	if err := actor.Validate(); err != nil {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "%s", err.Error())
	}
	return s.repository.Create(ctx, input, actor)
}

func (s *Service) Distill(ctx context.Context, input knowledge.CreateInput, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	if len(input.Provenance) == 0 {
		return knowledge.Item{}, knowledge.NewError(knowledge.CodeInvalid, "distilled knowledge requires provenance")
	}
	return s.Create(ctx, input, actor)
}

func (s *Service) Get(ctx context.Context, id string) (knowledge.Item, error) {
	return s.repository.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, id string, expected int64, patch knowledge.Patch, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	return s.repository.Update(ctx, id, expected, patch, actor)
}

func (s *Service) Promote(ctx context.Context, id string, expected int64, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	return s.repository.Promote(ctx, id, expected, actor)
}

func (s *Service) Review(ctx context.Context, id string, expected int64, note string, actor contextmodel.ActorSnapshot) (knowledge.Item, error) {
	return s.repository.Review(ctx, id, expected, note, actor)
}

func (s *Service) Supersede(ctx context.Context, id string, expected int64, replacementID string, actor contextmodel.ActorSnapshot) (knowledge.Item, knowledge.Item, error) {
	return s.repository.Supersede(ctx, id, expected, replacementID, actor)
}

func (s *Service) Catalog(ctx context.Context, projectID string, options knowledge.CatalogOptions) ([]knowledge.CatalogItem, error) {
	return s.repository.Catalog(ctx, projectID, options)
}

func (s *Service) Search(ctx context.Context, projectID string, options knowledge.SearchOptions) ([]knowledge.CatalogItem, error) {
	return s.repository.Search(ctx, projectID, options)
}

func (s *Service) Events(ctx context.Context, id string) ([]knowledge.Event, error) {
	return s.repository.Events(ctx, id)
}

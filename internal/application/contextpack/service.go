package contextpack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	contextapp "s26.dev/istok-cli/internal/application/context"
	indexingapp "s26.dev/istok-cli/internal/application/indexing"
	contextdomain "s26.dev/istok-cli/internal/context"
	contextpack "s26.dev/istok-cli/internal/contextpack"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/retrieval"
	"s26.dev/istok-cli/internal/task"
)

type ProjectResolver interface {
	Resolve(context.Context, string, bool) (project.Project, error)
}

type TaskRetriever interface {
	SearchTask(context.Context, project.Project, retrieval.TaskSearchRequest) ([]retrieval.SearchResult, indexingapp.Status, error)
}

type Service struct {
	contexts  *contextapp.Service
	projects  ProjectResolver
	retriever TaskRetriever
	now       func() time.Time
	newID     func() (string, error)
}

func NewService(contexts *contextapp.Service, projects ProjectResolver, retriever TaskRetriever) *Service {
	return &Service{contexts: contexts, projects: projects, retriever: retriever, now: time.Now, newID: func() (string, error) { value, err := uuid.NewV7(); return value.String(), err }}
}

func (s *Service) BuildForTask(ctx context.Context, value task.Task, options contextpack.BuildOptions) (contextpack.Package, error) {
	options.RetrievalOverrideReason = strings.TrimSpace(options.RetrievalOverrideReason)
	if options.ContextLimit < 0 || (options.WithoutRetrieval != (options.RetrievalOverrideReason != "")) {
		return contextpack.Package{}, fmt.Errorf("without_retrieval and override_reason must be set together")
	}

	records, err := s.contexts.BuildPackage(ctx, value.ProjectID, contextdomain.BuildOptions{Limit: options.ContextLimit})
	if err != nil {
		return contextpack.Package{}, err
	}
	result := contextpack.Package{SchemaVersion: contextpack.SchemaVersion, ProjectID: value.ProjectID, Records: append([]contextdomain.ContextPackageItem{}, records.Records...), Retrieval: []contextpack.Item{}, Metadata: contextpack.Metadata{WithoutRetrieval: options.WithoutRetrieval, OverrideReason: options.RetrievalOverrideReason}}
	if options.WithoutRetrieval {
		result.GeneratedAt = s.now().UTC()
		return result, result.Validate()
	}

	current, err := s.projects.Resolve(ctx, value.ProjectID, false)
	if err != nil {
		return contextpack.Package{}, err
	}
	results, _, err := s.retriever.SearchTask(ctx, current, retrieval.TaskSearchRequest{Title: value.Title, Description: value.Description, AcceptanceCriteria: value.AcceptanceCriteria, Notes: value.Notes})
	if err != nil {
		return contextpack.Package{}, err
	}
	result.Retrieval = make([]contextpack.Item, 0, len(results))
	for _, searchResult := range results {
		itemID, err := s.newID()
		if err != nil {
			return contextpack.Package{}, fmt.Errorf("generate retrieval item id: %w", err)
		}
		result.Retrieval = append(result.Retrieval, contextpack.FromSearchResult(itemID, searchResult))
	}
	result.GeneratedAt = s.now().UTC()

	return result, result.Validate()
}

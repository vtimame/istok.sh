package contextpack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	contextapp "github.com/vtimame/istok.sh/internal/application/context"
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	contextdomain "github.com/vtimame/istok.sh/internal/context"
	contextpack "github.com/vtimame/istok.sh/internal/contextpack"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/retrieval"
	"github.com/vtimame/istok.sh/internal/task"
)

type ProjectResolver interface {
	Resolve(context.Context, string, bool) (project.Project, error)
}

type TaskRetriever interface {
	SearchTask(context.Context, project.Project, retrieval.TaskSearchRequest) ([]retrieval.SearchResult, indexingapp.Status, error)
}

type KnowledgeCatalog interface {
	Catalog(context.Context, string, knowledge.CatalogOptions) ([]knowledge.CatalogItem, error)
}

type Service struct {
	contexts  *contextapp.Service
	projects  ProjectResolver
	retriever TaskRetriever
	knowledge KnowledgeCatalog
	now       func() time.Time
	newID     func() (string, error)
}

func NewService(contexts *contextapp.Service, projects ProjectResolver, retriever TaskRetriever, catalog KnowledgeCatalog) *Service {
	return &Service{contexts: contexts, projects: projects, retriever: retriever, knowledge: catalog, now: time.Now, newID: func() (string, error) { value, err := uuid.NewV7(); return value.String(), err }}
}

func (s *Service) BuildForTask(ctx context.Context, value task.Task, options contextpack.BuildOptions) (contextpack.Package, error) {
	options.RetrievalOverrideReason = strings.TrimSpace(options.RetrievalOverrideReason)
	options.ContextOverrideReason = strings.TrimSpace(options.ContextOverrideReason)
	if options.ContextLimit < 0 {
		return contextpack.Package{}, contextdomain.NewError(contextdomain.CodeInvalid, "context limit must be non-negative")
	}
	if !options.LegacyAllContext && options.ContextLimit > contextdomain.DefaultAbsoluteDurableItems {
		return contextpack.Package{}, contextdomain.NewError(contextdomain.CodeInvalid, "context limit must not exceed the absolute durable item limit of %d", contextdomain.DefaultAbsoluteDurableItems)
	}
	if options.WithoutRetrieval != (options.RetrievalOverrideReason != "") {
		return contextpack.Package{}, contextdomain.NewError(contextdomain.CodeInvalid, "without_retrieval and retrieval_override_reason must be set together")
	}
	if options.LegacyAllContext != (options.ContextOverrideReason != "") {
		return contextpack.Package{}, contextdomain.NewError(contextdomain.CodeInvalid, "all_context and context_override_reason must be set together")
	}

	selectionOptions := contextdomain.DefaultSelectionOptions()
	if options.ContextLimit > 0 {
		selectionOptions.MaxDurableItems = options.ContextLimit
		selectionOptions.ExpandAlwaysItems = false
		selectionOptions.ItemBudgetReason = contextdomain.ItemBudgetReasonExplicit
	}
	if options.LegacyAllContext {
		const practicallyUnlimited = int(^uint(0) >> 2)
		selectionOptions.MaxDurableBytes = practicallyUnlimited
		selectionOptions.MaxDurableItems = practicallyUnlimited
		selectionOptions.AbsoluteMaxItems = practicallyUnlimited
		selectionOptions.MaxRecordBytes = practicallyUnlimited
		selectionOptions.ExpandAlwaysItems = false
		selectionOptions.ItemBudgetReason = contextdomain.ItemBudgetReasonLegacyAll
		selectionOptions.IncludeAll = true
	}
	records, assembly, err := s.contexts.BuildTaskPackage(ctx, value.ProjectID, contextdomain.TaskContextQuery{
		Title:              value.Title,
		Description:        value.Description,
		AcceptanceCriteria: value.AcceptanceCriteria,
		Notes:              value.Notes,
		ExplicitRecordIDs:  append([]string{}, options.ExplicitContextIDs...),
	}, selectionOptions)
	if err != nil {
		return contextpack.Package{}, err
	}
	if options.LegacyAllContext {
		assembly.TotalBudgetBytes = int(^uint(0) >> 2)
		assembly.Warnings = append(assembly.Warnings, "bounded durable context disabled by audited legacy all-context override")
	}
	result := contextpack.Package{SchemaVersion: contextpack.SchemaVersion, ProjectID: value.ProjectID, Records: append([]contextdomain.ContextPackageItem{}, records.Records...), Retrieval: []contextpack.Item{}, Knowledge: []knowledge.BriefingItem{}, Metadata: contextpack.Metadata{WithoutRetrieval: options.WithoutRetrieval, OverrideReason: options.RetrievalOverrideReason, ContextOverrideReason: options.ContextOverrideReason, Assembly: assembly}}
	if s.knowledge != nil {
		catalog, err := s.knowledge.Catalog(ctx, value.ProjectID, knowledge.CatalogOptions{Statuses: []knowledge.Status{knowledge.StatusCurrent}, Limit: knowledge.MaxCatalogLimit})
		if err != nil {
			return contextpack.Package{}, err
		}
		result.Knowledge, result.Metadata.Knowledge = knowledge.BuildBriefing(catalog)
	} else {
		_, result.Metadata.Knowledge = knowledge.BuildBriefing(nil)
	}
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
		result.Metadata.Assembly.Usage.RetrievalBytes += len(searchResult.Snippet)
	}
	result.Metadata.Assembly.Usage.TotalBytes = result.Metadata.Assembly.Usage.DurableBytes + result.Metadata.Assembly.Usage.RetrievalBytes
	if !options.LegacyAllContext && result.Metadata.Assembly.Usage.RetrievalBytes > result.Metadata.Assembly.RetrievalBudgetBytes {
		return contextpack.Package{}, contextdomain.NewError(contextdomain.CodeConflict, "repository retrieval exceeds the %d-byte budget", result.Metadata.Assembly.RetrievalBudgetBytes)
	}
	if !options.LegacyAllContext && result.Metadata.Assembly.Usage.TotalBytes > result.Metadata.Assembly.TotalBudgetBytes {
		return contextpack.Package{}, contextdomain.NewError(contextdomain.CodeConflict, "assembled context exceeds the %d-byte total budget", result.Metadata.Assembly.TotalBudgetBytes)
	}
	result.GeneratedAt = s.now().UTC()

	return result, result.Validate()
}

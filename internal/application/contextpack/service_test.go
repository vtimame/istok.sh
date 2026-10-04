package contextpack

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contextapp "github.com/vtimame/istok.sh/internal/application/context"
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	contextpack "github.com/vtimame/istok.sh/internal/contextpack"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/retrieval"
	"github.com/vtimame/istok.sh/internal/task"
)

type contextRepository struct {
	values []contextmodel.ProjectContextRecord
}

func (r *contextRepository) Create(context.Context, contextmodel.CreateInput, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	return contextmodel.ProjectContextRecord{}, nil
}
func (r *contextRepository) Get(context.Context, string, bool) (contextmodel.ProjectContextRecord, error) {
	return contextmodel.ProjectContextRecord{}, nil
}
func (r *contextRepository) Update(context.Context, string, int64, contextmodel.Patch, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	return contextmodel.ProjectContextRecord{}, nil
}
func (r *contextRepository) Archive(context.Context, string, int64, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	return contextmodel.ProjectContextRecord{}, nil
}
func (r *contextRepository) Restore(context.Context, string, int64, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	return contextmodel.ProjectContextRecord{}, nil
}
func (r *contextRepository) List(context.Context, string, contextmodel.ListOptions) ([]contextmodel.ProjectContextRecord, error) {
	return r.values, nil
}
func (r *contextRepository) Search(context.Context, string, contextmodel.SearchOptions) ([]contextmodel.ProjectContextRecord, error) {
	return nil, nil
}
func (r *contextRepository) Events(context.Context, string) ([]contextmodel.ContextEvent, error) {
	return nil, nil
}

type projectResolver struct {
	value project.Project
	calls int
	err   error
}

func (r *projectResolver) Resolve(context.Context, string, bool) (project.Project, error) {
	r.calls++
	return r.value, r.err
}

type taskRetriever struct {
	values  []retrieval.SearchResult
	request retrieval.TaskSearchRequest
	calls   int
	err     error
}

func (r *taskRetriever) SearchTask(_ context.Context, _ project.Project, request retrieval.TaskSearchRequest) ([]retrieval.SearchResult, indexingapp.Status, error) {
	r.calls++
	r.request = request
	return r.values, indexingapp.Status{}, r.err
}

func TestBuildForTaskMapsTaskAndCreatesLocalOnlyItems(t *testing.T) {
	projectID := testID(t)
	contexts := contextapp.NewService(&contextRepository{values: []contextmodel.ProjectContextRecord{{ID: testID(t), ProjectID: projectID, Revision: 1, Kind: contextmodel.KindNote, Title: "note", Source: contextmodel.SourceUser, Visibility: contextmodel.VisibilityShared, Sensitivity: contextmodel.SensitivityNormal}}})
	resolver := &projectResolver{value: project.Project{ID: projectID}}
	retriever := &taskRetriever{values: []retrieval.SearchResult{{ContractVersion: retrieval.ContractVersion, ChunkID: "chunk", Path: "internal/unique.go", Language: "go", LineStart: 3, LineEnd: 5, ContentHash: hash, Snippet: "UniqueSymbol", MatchedTerms: nil, Provenance: nil, Reasons: nil}}}
	service := NewService(contexts, resolver, retriever, nil)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.FixedZone("x", 3600))
	service.now = func() time.Time { return now }
	service.newID = func() (string, error) { return testID(t), nil }
	value := task.Task{ID: testID(t), ProjectID: projectID, Title: "Title", Description: "Description", AcceptanceCriteria: "Acceptance", Notes: "Notes"}

	got, err := service.BuildForTask(context.Background(), value, contextpack.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if retriever.request != (retrieval.TaskSearchRequest{Title: "Title", Description: "Description", AcceptanceCriteria: "Acceptance", Notes: "Notes"}) {
		t.Fatalf("request = %#v", retriever.request)
	}
	if resolver.calls != 1 || retriever.calls != 1 || len(got.Retrieval) != 1 || got.Retrieval[0].Visibility != "local_only" {
		t.Fatalf("package = %#v", got)
	}
	if got.Retrieval[0].MatchedTerms == nil || got.Retrieval[0].Provenance == nil || got.Retrieval[0].Reasons == nil {
		t.Fatalf("retrieval arrays must be non-nil: %#v", got.Retrieval[0])
	}
	if !got.GeneratedAt.Equal(now.UTC()) {
		t.Fatalf("generated_at = %s", got.GeneratedAt)
	}
}

func TestBuildForTaskFailureAndOverride(t *testing.T) {
	projectID := testID(t)
	contexts := contextapp.NewService(&contextRepository{values: []contextmodel.ProjectContextRecord{{ID: testID(t), ProjectID: projectID, Revision: 1, Kind: contextmodel.KindNote, Title: "record", Source: contextmodel.SourceUser, Visibility: contextmodel.VisibilityShared, Sensitivity: contextmodel.SensitivityNormal}}})
	resolver := &projectResolver{value: project.Project{ID: projectID}}
	retriever := &taskRetriever{err: errors.New("index unavailable")}
	service := NewService(contexts, resolver, retriever, nil)
	value := task.Task{ID: testID(t), ProjectID: projectID, Title: "record"}
	if _, err := service.BuildForTask(context.Background(), value, contextpack.BuildOptions{}); !errors.Is(err, retriever.err) {
		t.Fatalf("error = %v", err)
	}
	if _, err := service.BuildForTask(context.Background(), value, contextpack.BuildOptions{WithoutRetrieval: true}); err == nil {
		t.Fatal("expected invalid override")
	}

	resolver.calls, retriever.calls = 0, 0
	got, err := service.BuildForTask(context.Background(), value, contextpack.BuildOptions{WithoutRetrieval: true, RetrievalOverrideReason: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 0 || retriever.calls != 0 || len(got.Records) != 1 || got.Retrieval == nil || len(got.Retrieval) != 0 || !got.Metadata.WithoutRetrieval {
		t.Fatalf("override package = %#v", got)
	}
}

func TestBuildForTaskAuditedAllContextSupportsLegacyOversizePackage(t *testing.T) {
	projectID := testID(t)
	values := make([]contextmodel.ProjectContextRecord, 0, 6)
	for index := 0; index < 6; index++ {
		values = append(values, contextmodel.ProjectContextRecord{
			ID: testID(t), ProjectID: projectID, Revision: 1, Kind: contextmodel.KindNote,
			Title: "legacy record", Body: strings.Repeat("x", 10*1024), Tags: []string{},
			Source: contextmodel.SourceUser, Visibility: contextmodel.VisibilityShared,
			Sensitivity: contextmodel.SensitivityNormal, Delivery: contextmodel.DeliveryManual,
		})
	}
	service := NewService(
		contextapp.NewService(&contextRepository{values: values}),
		&projectResolver{value: project.Project{ID: projectID}},
		&taskRetriever{},
		nil,
	)

	got, err := service.BuildForTask(context.Background(), task.Task{ID: testID(t), ProjectID: projectID}, contextpack.BuildOptions{
		LegacyAllContext: true, ContextOverrideReason: "migration audit",
		WithoutRetrieval: true, RetrievalOverrideReason: "offline preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != len(values) || got.Metadata.Assembly.Usage.TotalBytes <= contextmodel.DefaultTotalBudgetBytes || got.Metadata.ContextOverrideReason != "migration audit" {
		t.Fatalf("legacy all-context package = %#v", got)
	}
}

func TestBuildForTaskRejectsRetrievalOverBudget(t *testing.T) {
	projectID := testID(t)
	retriever := &taskRetriever{values: []retrieval.SearchResult{{
		ContractVersion: retrieval.ContractVersion, ChunkID: "chunk", Path: "large.go", Language: "go",
		LineStart: 1, LineEnd: 1, ContentHash: hash, Snippet: strings.Repeat("x", contextmodel.DefaultRetrievalBudgetBytes+1),
	}}}
	service := NewService(
		contextapp.NewService(&contextRepository{}),
		&projectResolver{value: project.Project{ID: projectID}},
		retriever,
		nil,
	)

	_, err := service.BuildForTask(context.Background(), task.Task{ID: testID(t), ProjectID: projectID, Title: "large"}, contextpack.BuildOptions{})
	if contextmodel.ErrorCode(err) != contextmodel.CodeConflict {
		t.Fatalf("error = %v", err)
	}
}

func TestRetrievalItemRejectsNoncanonicalIDAndNormalizesEmptySlices(t *testing.T) {
	item := retrieval.SearchResult{ContractVersion: retrieval.ContractVersion, ChunkID: "chunk", Path: "file.go", Language: "go", LineStart: 1, LineEnd: 1, ContentHash: hash, Snippet: "text"}
	value := contextpack.FromSearchResult(testID(t), item)
	if value.MatchedTerms == nil || value.Provenance == nil || value.Reasons == nil {
		t.Fatalf("slices = %#v", value)
	}
	value.ItemID = strings.ToUpper(value.ItemID)
	if err := value.Validate(); err == nil {
		t.Fatal("expected noncanonical UUIDv7 to be rejected")
	}
}

const hash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

func testID(t *testing.T) string {
	t.Helper()
	value, err := contextmodel.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

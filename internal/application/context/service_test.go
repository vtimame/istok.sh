package contextapp

import (
	"context"
	"strings"
	"testing"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
)

type fakeRepository struct {
	listValues  []contextmodel.ProjectContextRecord
	listOptions contextmodel.ListOptions
	getValue    contextmodel.ProjectContextRecord
	updateErr   error
	updateCall  bool
}

func (r *fakeRepository) Create(context.Context, contextmodel.CreateInput, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	panic("unexpected call")
}

func (r *fakeRepository) Get(context.Context, string, bool) (contextmodel.ProjectContextRecord, error) {
	return r.getValue, nil
}

func (r *fakeRepository) Update(context.Context, string, int64, contextmodel.Patch, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	r.updateCall = true
	return r.getValue, r.updateErr
}

func (r *fakeRepository) Archive(context.Context, string, int64, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	return r.getValue, nil
}

func (r *fakeRepository) Restore(context.Context, string, int64, contextmodel.ActorSnapshot) (contextmodel.ProjectContextRecord, error) {
	return r.getValue, nil
}

func (r *fakeRepository) List(_ context.Context, _ string, options contextmodel.ListOptions) ([]contextmodel.ProjectContextRecord, error) {
	r.listOptions = options
	return r.listValues, nil
}

func (r *fakeRepository) Search(context.Context, string, contextmodel.SearchOptions) ([]contextmodel.ProjectContextRecord, error) {
	return nil, nil
}

func (r *fakeRepository) Events(context.Context, string) ([]contextmodel.ContextEvent, error) {
	return nil, nil
}

func TestBuildPackageAssemblesSchemaAndDeterministicHash(t *testing.T) {
	projectID, err := contextmodel.NewID()
	if err != nil {
		t.Fatal(err)
	}
	recordID, err := contextmodel.NewID()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{
		listValues: []contextmodel.ProjectContextRecord{{
			ID:          recordID,
			ProjectID:   projectID,
			Revision:    2,
			Kind:        contextmodel.KindDecision,
			Title:       "Decision",
			Body:        strings.Repeat("body ", 40),
			Tags:        []string{"decision", "architecture"},
			Source:      contextmodel.SourceUser,
			Visibility:  contextmodel.VisibilityShared,
			Sensitivity: contextmodel.SensitivityNormal,
		}},
	}

	service := NewService(repository)
	got, err := service.BuildPackage(context.Background(), projectID, contextmodel.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildPackage() = %v", err)
	}
	if got.SchemaVersion != "2" || got.ProjectID != projectID {
		t.Fatalf("BuildPackage() = %#v", got)
	}
	if len(got.Records) != 1 {
		t.Fatalf("records = %d", len(got.Records))
	}
	if got.Records[0].Snippet == "" || len([]rune(got.Records[0].Snippet)) > 120 {
		t.Fatalf("snippet = %q", got.Records[0].Snippet)
	}
	if len(got.Records[0].ContentHash) != 64 {
		t.Fatalf("content hash = %q", got.Records[0].ContentHash)
	}
}

func TestBuildPackagePrioritizesEnabledInstructionsOutsideOrdinaryLimit(t *testing.T) {
	projectID := mustUUID(t)
	enabled := true
	disabled := false
	high := contextmodel.PriorityHigh
	low := contextmodel.PriorityLow
	critical := contextmodel.PriorityCritical
	scope := contextmodel.ScopeProject
	record := func(kind contextmodel.Kind, title string) contextmodel.ProjectContextRecord {
		return contextmodel.ProjectContextRecord{
			ID:          mustUUID(t),
			ProjectID:   projectID,
			Revision:    1,
			Kind:        kind,
			Title:       title,
			Source:      contextmodel.SourceUser,
			Visibility:  contextmodel.VisibilityShared,
			Sensitivity: contextmodel.SensitivityNormal,
		}
	}

	firstNote := record(contextmodel.KindNote, "first note")
	lowInstruction := record(contextmodel.KindInstruction, "low instruction")
	lowInstruction.Enabled, lowInstruction.Priority, lowInstruction.Scope = &enabled, &low, &scope
	disabledInstruction := record(contextmodel.KindInstruction, "disabled instruction")
	disabledInstruction.Enabled, disabledInstruction.Priority, disabledInstruction.Scope = &disabled, &critical, &scope
	highInstruction := record(contextmodel.KindInstruction, "high instruction")
	highInstruction.Enabled, highInstruction.Priority, highInstruction.Scope = &enabled, &high, &scope
	secondNote := record(contextmodel.KindNote, "second note")

	repository := &fakeRepository{listValues: []contextmodel.ProjectContextRecord{
		firstNote,
		lowInstruction,
		disabledInstruction,
		highInstruction,
		secondNote,
	}}
	got, err := NewService(repository).BuildPackage(context.Background(), projectID, contextmodel.BuildOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if repository.listOptions.Limit != 0 || repository.listOptions.IncludeDisabled {
		t.Fatalf("repository list options = %+v", repository.listOptions)
	}
	want := []string{highInstruction.ID, lowInstruction.ID, firstNote.ID}
	if len(got.Records) != len(want) {
		t.Fatalf("records = %+v", got.Records)
	}
	for i, id := range want {
		if got.Records[i].RecordID != id {
			t.Fatalf("record %d = %s, want %s", i, got.Records[i].RecordID, id)
		}
	}
	if got.Records[0].Enabled == nil || !*got.Records[0].Enabled || got.Records[0].Priority == nil || *got.Records[0].Priority != high {
		t.Fatalf("instruction policy = %+v", got.Records[0])
	}

	changed := highInstruction
	changed.Priority = &low
	firstHash, err := contextRecordHash(highInstruction)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := contextRecordHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatal("instruction policy must participate in content hash")
	}
}

func TestUpdateRequiresRevisionAndActorValidationBeforeMutation(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	_, err := service.Update(context.Background(), "bad-id", 1, contextmodel.Patch{}, contextmodel.ActorSnapshot{ID: "local-user", Kind: "user", Name: "User"})
	if contextmodel.ErrorCode(err) != contextmodel.CodeInvalid {
		t.Fatalf("Update() = %v", err)
	}
	if repository.updateCall {
		t.Fatalf("repository Update was called too early")
	}

	validTitle := "updated"
	if _, err := service.Update(context.Background(), mustUUID(t), 0, contextmodel.Patch{Title: &validTitle}, contextmodel.ActorSnapshot{ID: "local-user", Kind: "user", Name: "User"}); contextmodel.ErrorCode(err) != contextmodel.CodeInvalid {
		t.Fatalf("Update() = %v", err)
	}
	if repository.updateCall {
		t.Fatalf("repository Update should not be called when expected revision is invalid")
	}

	blankTitle := " "
	if _, err := service.Update(context.Background(), mustUUID(t), 1, contextmodel.Patch{Title: &blankTitle}, contextmodel.ActorSnapshot{ID: " ", Kind: "user", Name: "User"}); contextmodel.ErrorCode(err) != contextmodel.CodeInvalid {
		t.Fatalf("Update() = %v", err)
	}

	if repository.updateCall {
		t.Fatalf("repository Update should not be called when actor is invalid")
	}
}

func mustUUID(t *testing.T) string {
	t.Helper()
	id, err := contextmodel.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

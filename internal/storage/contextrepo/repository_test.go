package contextrepo

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/storage"
	"github.com/vtimame/istok.sh/internal/storage/projectrepo"
	"go.uber.org/fx"
)

func TestCreateListSearchByKindAndTags(t *testing.T) {
	ctx := context.Background()
	repository, _, p := newContextRepository(t)
	if !contextmodel.IsUUIDv7(p.ID) {
		t.Fatalf("project id is not UUIDv7: %q", p.ID)
	}
	note := createRecord(t, ctx, repository, p.ID, contextmodel.CreateInput{
		Kind:        contextmodel.KindNote,
		Title:       "Auth strategy",
		Body:        "Token signing and rotation",
		Tags:        []string{"security", "identity"},
		Source:      contextmodel.SourceUser,
		Visibility:  contextmodel.VisibilityShared,
		Sensitivity: contextmodel.SensitivityNormal,
	}, actor)
	decision := createRecord(t, ctx, repository, p.ID, contextmodel.CreateInput{
		Kind:        contextmodel.KindDecision,
		Title:       "Decision 1",
		Body:        "Keep old auth",
		Tags:        []string{"architecture"},
		Source:      contextmodel.SourceAgent,
		Visibility:  contextmodel.VisibilityLocalOnly,
		Sensitivity: contextmodel.SensitivityPrivate,
	}, actor)
	_, err := repository.Archive(ctx, decision.ID, decision.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}

	values, err := repository.List(ctx, p.ID, contextmodel.ListOptions{Kinds: []contextmodel.Kind{contextmodel.KindNote}})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(values); got != 1 || values[0].ID != note.ID {
		t.Fatalf("List() = %#v", values)
	}

	values, err = repository.Search(ctx, p.ID, contextmodel.SearchOptions{Query: "signing"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(values); got != 1 || values[0].ID != note.ID {
		t.Fatalf("Search() = %#v", values)
	}

	values, err = repository.List(ctx, p.ID, contextmodel.ListOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("List(includeDeleted) count = %d, want 2", len(values))
	}
}

func TestUpdateAndArchiveRestoreCASAndEvents(t *testing.T) {
	ctx := context.Background()
	repository, _, p := newContextRepository(t)
	record := createRecord(t, ctx, repository, p.ID, contextmodel.CreateInput{
		Kind:        contextmodel.KindNote,
		Title:       "before",
		Source:      contextmodel.SourceUser,
		Visibility:  contextmodel.VisibilityShared,
		Sensitivity: contextmodel.SensitivityNormal,
	}, actor)

	updatedTitle := "after"
	updated, err := repository.Update(ctx, record.ID, record.Revision, contextmodel.Patch{Title: &updatedTitle}, actor)
	if err != nil || updated.Title != updatedTitle || updated.Revision != 2 {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}

	_, err = repository.Update(ctx, record.ID, record.Revision, contextmodel.Patch{Title: &updatedTitle}, actor)
	if err == nil || contextmodel.ErrorCode(err) != contextmodel.CodeRevisionConflict {
		t.Fatalf("stale Update() = %v", err)
	}

	archived, err := repository.Archive(ctx, updated.ID, updated.Revision, actor)
	if err != nil || archived.DeletedAt == nil || archived.Revision != 3 {
		t.Fatalf("Archive() = %#v, %v", archived, err)
	}
	_, err = repository.Archive(ctx, updated.ID, record.Revision, actor)
	if err != nil {
		t.Fatalf("idempotent Archive() = %v", err)
	}

	restored, err := repository.Restore(ctx, archived.ID, archived.Revision, actor)
	if err != nil || restored.DeletedAt != nil || restored.Revision != 4 {
		t.Fatalf("Restore() = %#v, %v", restored, err)
	}
	_, err = repository.Restore(ctx, archived.ID, archived.Revision, actor)
	if err != nil {
		t.Fatalf("idempotent Restore() = %v", err)
	}

	events, err := repository.Events(ctx, record.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("Events() = %#v, %v", events, err)
	}
	for i, kind := range []string{"created", "updated", "archived", "restored"} {
		if events[i].Type != kind || events[i].Actor != actor {
			t.Fatalf("event %d = %#v", i, events[i])
		}
	}
}

func TestInstructionPolicyDefaultsAndDisabledFiltering(t *testing.T) {
	ctx := context.Background()
	repository, _, p := newContextRepository(t)
	instruction := createRecord(t, ctx, repository, p.ID, contextmodel.CreateInput{
		Kind:        contextmodel.KindInstruction,
		Title:       "Build policy",
		Body:        "Run tests before completion",
		Source:      contextmodel.SourceUser,
		Visibility:  contextmodel.VisibilityShared,
		Sensitivity: contextmodel.SensitivityNormal,
	}, actor)
	if instruction.Enabled == nil || !*instruction.Enabled || instruction.Priority == nil || *instruction.Priority != contextmodel.PriorityNormal || instruction.Scope == nil || *instruction.Scope != contextmodel.ScopeProject {
		t.Fatalf("instruction defaults = %+v", instruction)
	}

	note := createRecord(t, ctx, repository, p.ID, contextmodel.CreateInput{
		Kind:        contextmodel.KindNote,
		Title:       "Build note",
		Body:        "Run tests",
		Source:      contextmodel.SourceUser,
		Visibility:  contextmodel.VisibilityShared,
		Sensitivity: contextmodel.SensitivityNormal,
	}, actor)
	if note.Enabled != nil || note.Priority != nil || note.Scope != nil {
		t.Fatalf("note exposes instruction policy = %+v", note)
	}

	disabled := false
	updated, err := repository.Update(ctx, instruction.ID, instruction.Revision, contextmodel.Patch{Enabled: &disabled}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled == nil || *updated.Enabled || updated.Revision != instruction.Revision+1 {
		t.Fatalf("disabled instruction = %+v", updated)
	}

	listed, err := repository.List(ctx, p.ID, contextmodel.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != note.ID {
		t.Fatalf("default list = %+v", listed)
	}
	listed, err = repository.List(ctx, p.ID, contextmodel.ListOptions{IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("include-disabled list = %+v", listed)
	}

	searched, err := repository.Search(ctx, p.ID, contextmodel.SearchOptions{Query: "tests"})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 1 || searched[0].ID != note.ID {
		t.Fatalf("default search = %+v", searched)
	}
	searched, err = repository.Search(ctx, p.ID, contextmodel.SearchOptions{Query: "tests", IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 2 {
		t.Fatalf("include-disabled search = %+v", searched)
	}
}

func TestConcurrentCASUpdatesAreConflictAware(t *testing.T) {
	ctx := context.Background()
	repository, _, p := newContextRepository(t)
	value := createRecord(t, ctx, repository, p.ID, contextmodel.CreateInput{
		Kind:        contextmodel.KindInstruction,
		Title:       "concurrent",
		Source:      contextmodel.SourceUser,
		Visibility:  contextmodel.VisibilityShared,
		Sensitivity: contextmodel.SensitivityNormal,
	}, actor)

	const count = 12
	successes := make(chan struct{}, count)
	failures := make(chan error, count)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			title := "updated"
			_, err := repository.Update(ctx, value.ID, value.Revision, contextmodel.Patch{Title: &title}, actor)
			if err == nil {
				successes <- struct{}{}
				return
			}
			failures <- err
		}()
	}

	close(start)
	wg.Wait()
	close(successes)
	close(failures)

	var successCount int
	for range successes {
		successCount++
	}
	var staleConflicts int
	for err := range failures {
		if err == nil {
			successCount++
			continue
		}
		if contextmodel.ErrorCode(err) != contextmodel.CodeRevisionConflict {
			t.Fatalf("Update() = %v", err)
		}
		staleConflicts++
	}
	if successCount != 1 || staleConflicts != count-1 {
		t.Fatalf("concurrent updates: success=%d conflicts=%d", successCount, staleConflicts)
	}

	got, err := repository.Get(ctx, value.ID, false)
	if err != nil || got.Revision != 2 {
		t.Fatalf("Get() = %#v, %v", got, err)
	}
}

func TestMigrationConstraintsAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	repository, _, p := newContextRepository(t)
	db := repository.db
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := db.ExecContext(ctx, `INSERT INTO context_records(id,project_id,revision,kind,title,body,tags,source,visibility,sensitivity,actor_id,actor_kind,actor_name,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, mustID(t), p.ID, 1, "bad-kind", "bad", "", "[]", "user", "shared", "normal", actor.ID, actor.Kind, actor.Name, now, now)
	if err == nil {
		t.Fatal("invalid kind accepted")
	}
	_, err = db.ExecContext(ctx, `INSERT INTO context_records(id,project_id,revision,kind,title,body,tags,source,visibility,sensitivity,actor_id,actor_kind,actor_name,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, mustID(t), p.ID, 1, "note", "", "{}", "[]", "user", "shared", "normal", actor.ID, actor.Kind, actor.Name, now, now)
	if err == nil {
		t.Fatal("empty title accepted")
	}
	_, err = db.ExecContext(ctx, `INSERT INTO context_records(id,project_id,revision,kind,title,body,tags,source,visibility,sensitivity,actor_id,actor_kind,actor_name,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, mustID(t), p.ID, 1, "note", "x", "", "{", "user", "shared", "normal", actor.ID, actor.Kind, actor.Name, now, now)
	if err == nil {
		t.Fatal("invalid json tags accepted")
	}
}

func createRecord(t *testing.T, ctx context.Context, repository *Repository, projectID string, input contextmodel.CreateInput, actor contextmodel.ActorSnapshot) contextmodel.ProjectContextRecord {
	t.Helper()

	input.ProjectID = projectID
	record, err := repository.Create(ctx, input, actor)
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	return record
}

func newContextRepository(t *testing.T) (*Repository, *project.Service, project.Project) {
	t.Helper()
	database := filepath.Join(t.TempDir(), "istok.db")
	projects := newProjects(t, database)
	return newRepository(t, database), projects, initProject(t, projects, "context-project")
}

func newProjects(t *testing.T, database string) *project.Service {
	t.Helper()
	var service *project.Service
	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: database}),
		fx.Provide(projectrepo.New),
		fx.Provide(func(repository *projectrepo.Repository) project.Repository {
			return repository
		}),
		fx.Provide(project.NewService),
		fx.Invoke(func(value *project.Service) {
			service = value
		}),
	)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start migrated SQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Errorf("stop SQLite: %v", err)
		}
	})
	return service
}

func newRepository(t *testing.T, database string) *Repository {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+database+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return New(db)
}

func initProject(t *testing.T, service *project.Service, name string) project.Project {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	value, err := service.Init(context.Background(), path, name)
	if err != nil {
		t.Fatal(err)
	}
	return value.Project
}

func mustID(t *testing.T) string {
	t.Helper()
	id, err := contextmodel.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var actor = contextmodel.ActorSnapshot{ID: "local-user", Kind: "user", Name: "Local User"}

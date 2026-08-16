package taskrepo

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"go.uber.org/fx"

	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/storage"
	"s26.dev/istok-cli/internal/storage/projectrepo"
	"s26.dev/istok-cli/internal/task"
)

var actor = task.ActorSnapshot{ID: "local-user", Kind: "user", Name: "Local User"}

func TestCreateConcurrentNumbersAndProjectCounters(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "istok.db")
	projects := newProjects(t, database)
	first := initProject(t, projects, "one")
	second := initProject(t, projects, "two")
	left := newRepository(t, database)
	right := newRepository(t, database)
	repositories := []*Repository{left, right}

	const count = 20
	values := make(chan task.Task, count)
	errs := make(chan error, count)
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func(repo *Repository, title string) {
			defer group.Done()
			<-start
			value, err := repo.Create(ctx, task.CreateInput{ProjectID: first.ID, Title: title}, actor)
			values <- value
			errs <- err
		}(repositories[i%len(repositories)], "task")
	}
	close(start)
	group.Wait()
	close(values)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var numbers []int
	for value := range values {
		if !task.IsUUIDv7(value.ID) {
			t.Fatalf("id %q is not UUIDv7", value.ID)
		}
		numbers = append(numbers, int(value.Number))
	}
	sort.Ints(numbers)
	for i, number := range numbers {
		if number != i+1 {
			t.Fatalf("numbers = %v", numbers)
		}
	}
	other, err := left.Create(ctx, task.CreateInput{ProjectID: second.ID, Title: "first"}, actor)
	if err != nil || other.Number != 1 {
		t.Fatalf("other project task = %#v, %v", other, err)
	}
}

func TestDeleteRestoreCASAndEvents(t *testing.T) {
	ctx := context.Background()
	repo, _, p := newTaskRepository(t)
	created, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "  task  "}, actor)
	if err != nil || created.Title != "task" || created.Number != 1 || created.Revision != 1 {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	deleted, err := repo.Delete(ctx, created.ID, created.Revision, actor)
	if err != nil || deleted.DeletedAt == nil || deleted.Revision != 2 {
		t.Fatalf("Delete() = %#v, %v", deleted, err)
	}
	if _, err := repo.Get(ctx, created.ID, false); task.ErrorCode(err) != task.CodeNotFound {
		t.Fatalf("Get active error = %v", err)
	}
	if _, err := repo.Delete(ctx, created.ID, created.Revision, actor); err != nil {
		t.Fatalf("idempotent Delete() = %v", err)
	}
	next, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "next"}, actor)
	if err != nil || next.Number != 2 {
		t.Fatalf("next Create() = %#v, %v", next, err)
	}

	if _, err := repo.Restore(ctx, created.ID, created.Revision, actor); task.ErrorCode(err) != task.CodeRevisionConflict {
		t.Fatalf("stale Restore() = %v", err)
	}
	restored, err := repo.Restore(ctx, created.ID, deleted.Revision, actor)
	if err != nil || restored.DeletedAt != nil || restored.Revision != 3 || restored.Number != 1 {
		t.Fatalf("Restore() = %#v, %v", restored, err)
	}
	events, err := repo.Events(ctx, created.ID)
	if err != nil || len(events) != 3 {
		t.Fatalf("Events() = %#v, %v", events, err)
	}
	for i, kind := range []string{"created", "deleted", "restored"} {
		if events[i].Type != kind || events[i].TaskRevision != int64(i+1) || events[i].Actor != actor {
			t.Fatalf("event %d = %#v", i, events[i])
		}
	}
}

func TestCreateRejectsDeletedProjectAndDoesNotAllocate(t *testing.T) {
	ctx := context.Background()
	repo, projects, p := newTaskRepository(t)
	deleted, err := projects.Delete(ctx, p.ID, &p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(ctx, task.CreateInput{ProjectID: deleted.ID, Title: "forbidden"}, actor)
	if task.ErrorCode(err) != task.CodeConflict {
		t.Fatalf("Create deleted project error = %v", err)
	}

	restored, err := projects.Restore(ctx, deleted.ID, "", &deleted.Revision)
	if err != nil {
		t.Fatal(err)
	}

	created, err := repo.Create(ctx, task.CreateInput{ProjectID: restored.ID, Title: "allowed"}, actor)
	if err != nil || created.Number != 1 {
		t.Fatalf("Create restored project task = %#v, %v", created, err)
	}
}

func TestFailedCreateAfterAllocationDoesNotBurnNumber(t *testing.T) {
	ctx := context.Background()
	repo, _, p := newTaskRepository(t)
	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}

	first, err := repo.Create(ctx, task.CreateInput{ID: id, ProjectID: p.ID, Title: "first"}, actor)
	if err != nil || first.Number != 1 {
		t.Fatalf("first Create() = %#v, %v", first, err)
	}

	_, err = repo.Create(ctx, task.CreateInput{ID: id, ProjectID: p.ID, Title: "duplicate"}, actor)
	assertError(t, err, task.CodeConflict)

	next, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "next"}, actor)
	if err != nil || next.Number != 2 {
		t.Fatalf("next Create() = %#v, %v; want number 2", next, err)
	}
}

func TestEventsRejectsMissingTask(t *testing.T) {
	repo, _, _ := newTaskRepository(t)
	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.Events(context.Background(), id)
	assertError(t, err, task.CodeNotFound)
}

func TestMigrationConstraintsAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	repo, _, p := newTaskRepository(t)
	value, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "valid"}, actor)
	if err != nil {
		t.Fatal(err)
	}

	db := repo.db
	_, err = db.ExecContext(ctx, `INSERT INTO tasks(id,project_id,number,status,title,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, mustID(t), p.ID, 0, "open", "title", "now", "now")
	if err == nil {
		t.Fatal("task number zero accepted")
	}

	_, err = db.ExecContext(ctx, `INSERT INTO tasks(id,project_id,number,status,title,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, mustID(t), p.ID, value.Number, "open", "title", "now", "now")
	if err == nil {
		t.Fatal("duplicate project task number accepted")
	}

	_, err = db.ExecContext(ctx, `INSERT INTO tasks(id,project_id,number,status,title,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, mustID(t), p.ID, 2, "wrong", "title", "now", "now")
	if err == nil {
		t.Fatal("invalid task status accepted")
	}

	_, err = db.ExecContext(ctx, `INSERT INTO task_events(id,task_id,type,body,task_revision,actor_id,actor_kind,actor_name,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, mustID(t), value.ID, "", "", 1, actor.ID, actor.Kind, actor.Name, "now")
	if err == nil {
		t.Fatal("empty event type accepted")
	}

	_, err = db.ExecContext(ctx, `INSERT INTO task_events(id,task_id,type,body,task_revision,actor_id,actor_kind,actor_name,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, mustID(t), mustID(t), "created", "", 1, actor.ID, actor.Kind, actor.Name, "now")
	if err == nil {
		t.Fatal("missing event task foreign key accepted")
	}
}

func newTaskRepository(t *testing.T) (*Repository, *project.Service, project.Project) {
	t.Helper()
	database := filepath.Join(t.TempDir(), "istok.db")
	projects := newProjects(t, database)
	return newRepository(t, database), projects, initProject(t, projects, "task-project")
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
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Error(err)
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
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

func initProject(t *testing.T, service *project.Service, name string) project.Project {
	t.Helper()
	path := t.TempDir()
	value, err := service.Init(context.Background(), path, name)
	if err != nil {
		t.Fatal(err)
	}
	return value.Project
}

func assertError(t *testing.T, err error, want task.Code) {
	t.Helper()
	var value *task.Error
	if !errors.As(err, &value) || value.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func mustID(t *testing.T) string {
	t.Helper()

	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}

	return id
}

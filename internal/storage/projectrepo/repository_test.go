package projectrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/storage"
)

func TestRepositoryInitConvergesAcrossIndependentDatabaseHandles(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "istok.db")
	root := makeDirectory(t, t.TempDir(), "workspace")
	first := newService(t, database)
	second := newService(t, database)

	results := make(chan project.InitResult, 2)
	errs := make(chan error, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for _, service := range []*project.Service{first, second} {
		group.Add(1)
		go func(service *project.Service) {
			defer group.Done()
			<-start
			result, err := service.Init(ctx, root, "shared")
			results <- result
			errs <- err
		}(service)
	}
	close(start)
	group.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Init() error = %v", err)
		}
	}
	var initialized []project.InitResult
	for result := range results {
		initialized = append(initialized, result)
	}
	if len(initialized) != 2 {
		t.Fatalf("Init results = %d, want 2", len(initialized))
	}
	if initialized[0].Project.ID != initialized[1].Project.ID {
		t.Fatalf("concurrent IDs = %q and %q, want one project", initialized[0].Project.ID, initialized[1].Project.ID)
	}
	parsed, err := uuid.Parse(initialized[0].Project.ID)
	if err != nil || parsed.Version() != 7 {
		t.Fatalf("project ID = %q, want UUIDv7 (err = %v)", initialized[0].Project.ID, err)
	}
	if initialized[0].Created == initialized[1].Created {
		t.Fatalf("created flags = %v and %v, want exactly one creation", initialized[0].Created, initialized[1].Created)
	}

	projects, err := first.List(ctx, false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(projects) != 1 || projects[0].Root == nil || projects[0].Root.CanonicalPath != root {
		t.Fatalf("active projects = %#v, want one root %q", projects, root)
	}
}

func TestRepositoryAllowsNestedProjectsInEitherInitializationOrder(t *testing.T) {
	for _, childFirst := range []bool{false, true} {
		name := "parent first"
		if childFirst {
			name = "child first"
		}

		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			service := newService(t, filepath.Join(t.TempDir(), "istok.db"))
			parent := makeDirectory(t, t.TempDir(), "parent")
			child := makeDirectory(t, parent, "child")
			grandchild := makeDirectory(t, child, "grandchild")

			paths := []string{parent, child}
			names := []string{"parent", "child"}
			if childFirst {
				paths[0], paths[1] = paths[1], paths[0]
				names[0], names[1] = names[1], names[0]
			}

			projects := make(map[string]project.Project, 2)
			for i, path := range paths {
				result, err := service.Init(ctx, path, names[i])
				if err != nil {
					t.Fatalf("Init(%q) error = %v", path, err)
				}
				if !result.Created {
					t.Fatalf("Init(%q) created = false, want a distinct nested project", path)
				}
				projects[path] = result.Project
			}
			if projects[parent].ID == projects[child].ID {
				t.Fatalf("parent and child IDs = %q, want distinct projects", projects[parent].ID)
			}

			for _, path := range []string{parent, child} {
				repeated, err := service.Init(ctx, path, "ignored")
				if err != nil {
					t.Fatalf("repeat Init(%q) error = %v", path, err)
				}
				if repeated.Created || repeated.Project.ID != projects[path].ID {
					t.Fatalf("repeat Init(%q) = %#v, want existing project %q", path, repeated, projects[path].ID)
				}
			}

			for path, expected := range map[string]string{
				parent:     projects[parent].ID,
				child:      projects[child].ID,
				grandchild: projects[child].ID,
			} {
				current, err := service.Current(ctx, path)
				if err != nil || current.ID != expected {
					t.Fatalf("Current(%q) = %#v, %v; want %q", path, current, err, expected)
				}
			}
		})
	}
}

func TestRepositoryRebindAndRestoreAllowNestedRoots(t *testing.T) {
	ctx := context.Background()
	service := newService(t, filepath.Join(t.TempDir(), "istok.db"))
	parent := makeDirectory(t, t.TempDir(), "parent")
	child := makeDirectory(t, parent, "child")
	other := makeDirectory(t, t.TempDir(), "other")

	parentProject, err := service.Init(ctx, parent, "parent")
	if err != nil {
		t.Fatal(err)
	}
	childProject, err := service.Init(ctx, other, "child")
	if err != nil {
		t.Fatal(err)
	}

	rebound, err := service.Rebind(ctx, childProject.Project.ID, child, &childProject.Project.Revision)
	if err != nil || rebound.Root == nil || rebound.Root.CanonicalPath != child {
		t.Fatalf("Rebind(child) = %#v, %v", rebound, err)
	}
	current, err := service.Current(ctx, child)
	if err != nil || current.ID != rebound.ID {
		t.Fatalf("Current(child) = %#v, %v; want %q", current, err, rebound.ID)
	}

	deleted, err := service.Delete(ctx, rebound.ID, &rebound.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.Restore(ctx, deleted.ID, child, &deleted.Revision)
	if err != nil || restored.Root == nil || restored.Root.CanonicalPath != child {
		t.Fatalf("Restore(child) = %#v, %v", restored, err)
	}

	_, err = service.Rebind(ctx, restored.ID, parent, &restored.Revision)
	assertCode(t, err, project.CodeConflict)
	parentCurrent, err := service.Current(ctx, parent)
	if err != nil || parentCurrent.ID != parentProject.Project.ID {
		t.Fatalf("Current(parent) after exact conflict = %#v, %v; want %q", parentCurrent, err, parentProject.Project.ID)
	}
	childCurrent, err := service.Current(ctx, child)
	if err != nil || childCurrent.ID != restored.ID {
		t.Fatalf("Current(child) after exact conflict = %#v, %v; want %q", childCurrent, err, restored.ID)
	}
}

func TestRepositoryCanonicalizesSymlinkUnicodeAndSpacesWithoutChangingWorkingDirectory(t *testing.T) {
	ctx := context.Background()
	service := newService(t, filepath.Join(t.TempDir(), "istok.db"))
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	target := makeDirectory(t, t.TempDir(), "пространство name")
	alias := filepath.Join(filepath.Dir(target), "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	result, err := service.Init(ctx, alias, "")
	if err != nil {
		t.Fatalf("Init(symlink) error = %v", err)
	}
	if result.Project.Root == nil || result.Project.Root.CanonicalPath != target {
		t.Fatalf("canonical root = %#v, want %q", result.Project.Root, target)
	}
	if result.Project.Name != filepath.Base(target) {
		t.Fatalf("default name = %q, want %q", result.Project.Name, filepath.Base(target))
	}
	if got, err := os.Getwd(); err != nil || got != workingDirectory {
		t.Fatalf("working directory = %q, %v; want unchanged %q", got, err, workingDirectory)
	}
}

func TestRepositoryAllowsDuplicateNamesButRejectsAmbiguousSelector(t *testing.T) {
	ctx := context.Background()
	service := newService(t, filepath.Join(t.TempDir(), "istok.db"))
	firstRoot := makeDirectory(t, t.TempDir(), "one")
	secondRoot := makeDirectory(t, t.TempDir(), "two")
	first, err := service.Init(ctx, firstRoot, "same")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Init(ctx, secondRoot, "same")
	if err != nil {
		t.Fatal(err)
	}
	if first.Project.ID == second.Project.ID {
		t.Fatal("duplicate names unexpectedly identified one project")
	}
	_, err = service.Resolve(ctx, "same", false)
	assertCode(t, err, project.CodeAmbiguous)
}

func TestRepositoryRenameRebindRevisionAndCAS(t *testing.T) {
	ctx := context.Background()
	service := newService(t, filepath.Join(t.TempDir(), "istok.db"))
	root := makeDirectory(t, t.TempDir(), "one")
	other := makeDirectory(t, t.TempDir(), "two")
	initialized, err := service.Init(ctx, root, "before")
	if err != nil {
		t.Fatal(err)
	}
	revision := initialized.Project.Revision

	renamed, err := service.Rename(ctx, initialized.Project.ID, "after", &revision)
	if err != nil || renamed.Revision != revision+1 {
		t.Fatalf("Rename() = %#v, %v; want revision %d", renamed, err, revision+1)
	}
	idempotent, err := service.Rename(ctx, initialized.Project.ID, "after", &revision)
	if err != nil || idempotent.Revision != renamed.Revision {
		t.Fatalf("idempotent Rename() = %#v, %v", idempotent, err)
	}
	_, err = service.Rename(ctx, initialized.Project.ID, "stale", &revision)
	assertCode(t, err, project.CodeRevisionConflict)

	rebound, err := service.Rebind(ctx, initialized.Project.ID, other, &renamed.Revision)
	if err != nil || rebound.Revision != renamed.Revision+1 {
		t.Fatalf("Rebind() = %#v, %v; want revision %d", rebound, err, renamed.Revision+1)
	}
	idempotent, err = service.Rebind(ctx, initialized.Project.ID, other, &renamed.Revision)
	if err != nil || idempotent.Revision != rebound.Revision {
		t.Fatalf("idempotent Rebind() = %#v, %v", idempotent, err)
	}
}

func TestRepositoryDeleteReleasesPathAndListsHistoricalRoot(t *testing.T) {
	ctx := context.Background()
	service := newService(t, filepath.Join(t.TempDir(), "istok.db"))
	root := makeDirectory(t, t.TempDir(), "root")
	initialized, err := service.Init(ctx, root, "old")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := service.Delete(ctx, initialized.Project.ID, &initialized.Project.Revision)
	if err != nil || deleted.DeletedAt == nil || deleted.Revision != initialized.Project.Revision+1 {
		t.Fatalf("Delete() = %#v, %v", deleted, err)
	}
	if _, err := service.Current(ctx, root); project.ErrorCode(err) != project.CodeNotFound {
		t.Fatalf("Current(deleted root) error = %v, want not found", err)
	}
	repeated, err := service.Delete(ctx, initialized.Project.ID, &initialized.Project.Revision)
	if err != nil || repeated.Revision != deleted.Revision {
		t.Fatalf("idempotent Delete() = %#v, %v", repeated, err)
	}

	replacement, err := service.Init(ctx, root, "new")
	if err != nil || !replacement.Created {
		t.Fatalf("Init(released path) = %#v, %v", replacement, err)
	}
	history, err := service.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var historical *project.Project
	for i := range history {
		if history[i].ID == initialized.Project.ID {
			historical = &history[i]
		}
	}
	if historical == nil || historical.Root == nil || historical.Root.CanonicalPath != root || historical.Root.DetachedAt == nil {
		t.Fatalf("historical deleted project = %#v, want detached root %q", historical, root)
	}
}

func TestRepositoryRestoreSameMissingMovedAndConflictingPaths(t *testing.T) {
	ctx := context.Background()
	service := newService(t, filepath.Join(t.TempDir(), "istok.db"))
	root := makeDirectory(t, t.TempDir(), "root")
	moved := makeDirectory(t, t.TempDir(), "moved")
	different := makeDirectory(t, t.TempDir(), "different")

	initialized, err := service.Init(ctx, root, "restore")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := service.Delete(ctx, initialized.Project.ID, &initialized.Project.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.Restore(ctx, initialized.Project.ID, "", &deleted.Revision)
	if err != nil || restored.Root == nil || restored.Root.CanonicalPath != root {
		t.Fatalf("Restore(same path) = %#v, %v", restored, err)
	}
	repeated, err := service.Restore(ctx, initialized.Project.ID, "", &deleted.Revision)
	if err != nil || repeated.Revision != restored.Revision {
		t.Fatalf("idempotent Restore() = %#v, %v", repeated, err)
	}

	deleted, err = service.Delete(ctx, initialized.Project.ID, &restored.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove old root: %v", err)
	}
	_, err = service.Restore(ctx, initialized.Project.ID, "", &deleted.Revision)
	assertCode(t, err, project.CodeConflict)
	restored, err = service.Restore(ctx, initialized.Project.ID, moved, &deleted.Revision)
	if err != nil || restored.Root == nil || restored.Root.CanonicalPath != moved {
		t.Fatalf("Restore(moved path) = %#v, %v", restored, err)
	}
	repeated, err = service.Restore(ctx, initialized.Project.ID, moved, &deleted.Revision)
	if err != nil || repeated.Revision != restored.Revision {
		t.Fatalf("idempotent Restore(moved path) = %#v, %v", repeated, err)
	}
	_, err = service.Restore(ctx, initialized.Project.ID, different, &deleted.Revision)
	assertCode(t, err, project.CodeRevisionConflict)

	deleted, err = service.Delete(ctx, initialized.Project.ID, &restored.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("recreate old root: %v", err)
	}
	if _, err := service.Init(ctx, root, "occupies old path"); err != nil {
		t.Fatal(err)
	}
	_, err = service.Restore(ctx, initialized.Project.ID, root, &deleted.Revision)
	assertCode(t, err, project.CodeConflict)
}

func newService(t *testing.T, database string) *project.Service {
	t.Helper()
	var service *project.Service
	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: database}),
		fx.Provide(New),
		fx.Provide(func(repository *Repository) project.Repository { return repository }),
		fx.Provide(project.NewService),
		fx.Invoke(func(value *project.Service) { service = value }),
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

func makeDirectory(t *testing.T, elements ...string) string {
	t.Helper()
	path := filepath.Join(elements...)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create directory %q: %v", path, err)
	}
	return path
}

func assertCode(t *testing.T, err error, want project.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", want)
	}
	var projectErr *project.Error
	if !errors.As(err, &projectErr) || projectErr.Code != want {
		t.Fatalf("error = %v (code %s), want %s", err, project.ErrorCode(err), want)
	}
}

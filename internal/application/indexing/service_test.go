package indexing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/vtimame/istok.sh/internal/indexing/discovery"
	"github.com/vtimame/istok.sh/internal/indexing/manifest"
	"github.com/vtimame/istok.sh/internal/indexing/sidecar"
	"github.com/vtimame/istok.sh/internal/indexstore"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/retrieval"
)

func TestIndexingStatusBeforeInitialBuild(t *testing.T) {
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, nil)

	status, err := service.Status(context.Background(), projectValue)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != sidecar.StateNeverIndexed {
		t.Fatalf("Status() state = %q, want %q", status.State, sidecar.StateNeverIndexed)
	}
	if status.EpochID != "" {
		t.Fatalf("Status() epoch_id = %q, want empty", status.EpochID)
	}
}

func TestEnsureFreshInitialBuildCreatesReadyIndexAndSearchFindsText(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"main.go": `package main

func readyToken() string { return "hello-index" }
`,
	})

	status, err := service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() error = %v", err)
	}
	if status.State != sidecar.StateReady {
		t.Fatalf("EnsureFresh() state = %q, want %q", status.State, sidecar.StateReady)
	}
	if status.Revision != 1 {
		t.Fatalf("EnsureFresh() revision = %d, want 1", status.Revision)
	}

	results, searchStatus, err := service.Search(ctx, projectValue, retrieval.SearchRequest{Query: "readyToken"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if searchStatus.State != sidecar.StateReady {
		t.Fatalf("Search() state = %q, want %q", searchStatus.State, sidecar.StateReady)
	}
	if searchStatus.Revision != 1 {
		t.Fatalf("Search() revision = %d, want 1", searchStatus.Revision)
	}
	if len(results) != 1 {
		t.Fatalf("Search() result len = %d, want 1", len(results))
	}
	if results[0].Path != "main.go" {
		t.Fatalf("Search() result path = %q, want main.go", results[0].Path)
	}
}

func TestNestedProjectsKeepSeparateIndexesWithOverlappingCoverage(t *testing.T) {
	ctx := context.Background()
	indexRoot := t.TempDir()
	service := NewService(Config{IndexRoot: indexRoot})
	parentRoot := t.TempDir()
	childRoot := filepath.Join(parentRoot, "packages", "api")
	writeFile(t, parentRoot, "root.go", "package root\n\nconst rootonlysentinel8f3a6c2b = true\n")
	writeFile(t, parentRoot, "packages/api/api.go", "package api\n\nfunc nestedPackageToken() {}\n")

	parentCanonical, err := project.Canonicalize(parentRoot)
	if err != nil {
		t.Fatal(err)
	}
	childCanonical, err := project.Canonicalize(childRoot)
	if err != nil {
		t.Fatal(err)
	}
	parentProject := project.Project{ID: uuid.NewString(), Root: &parentCanonical}
	childProject := project.Project{ID: uuid.NewString(), Root: &childCanonical}

	for _, value := range []project.Project{parentProject, childProject} {
		status, err := service.EnsureFresh(ctx, value)
		if err != nil || status.State != sidecar.StateReady {
			t.Fatalf("EnsureFresh(%q) = %+v, %v", value.Root.CanonicalPath, status, err)
		}
		if _, err := os.Stat(filepath.Join(indexRoot, value.ID)); err != nil {
			t.Fatalf("project-scoped index %q: %v", value.ID, err)
		}
	}

	parentResults, _, err := service.Search(ctx, parentProject, retrieval.SearchRequest{Query: "nestedPackageToken"})
	if err != nil || !hasSearchPath(parentResults, "packages/api/api.go") {
		t.Fatalf("parent search = %+v, %v; want nested package result", parentResults, err)
	}
	childResults, _, err := service.Search(ctx, childProject, retrieval.SearchRequest{Query: "nestedPackageToken"})
	if err != nil || !hasSearchPath(childResults, "api.go") {
		t.Fatalf("child search = %+v, %v; want package-relative result", childResults, err)
	}
	outsideResults, _, err := service.Search(ctx, childProject, retrieval.SearchRequest{Query: "rootonlysentinel8f3a6c2b"})
	if err != nil || len(outsideResults) != 0 {
		t.Fatalf("child search outside its root = %+v, %v; want no results", outsideResults, err)
	}
}

func hasSearchPath(results []retrieval.SearchResult, path string) bool {
	for _, result := range results {
		if result.Path == path {
			return true
		}
	}

	return false
}

func TestEnsureFreshUnchangedBuildDoesNotReadSource(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"main.go": `package main

func cachedBuild() int { return 10 }
`,
	})

	status, err := service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() initial error = %v", err)
	}
	revision := status.Revision

	var readCalls int64
	service.readFile = func(_ string) ([]byte, error) {
		atomic.AddInt64(&readCalls, 1)
		return nil, errors.New("read forbidden during confirmation")
	}

	status, err = service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() repeated error = %v", err)
	}
	if status.Revision != revision {
		t.Fatalf("EnsureFresh() revision = %d, want %d", status.Revision, revision)
	}
	if status.State != sidecar.StateReady {
		t.Fatalf("EnsureFresh() state = %q, want %q", status.State, sidecar.StateReady)
	}
	if calls := atomic.LoadInt64(&readCalls); calls != 0 {
		t.Fatalf("read file calls = %d, want 0", calls)
	}
}

func TestEnsureFreshIncrementalAddModifyDeleteRename(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"a.go": `package main

func alpha() { _ = 1 }
`,
		"b.go": `package main

func beta() { _ = 2 }
`,
	})

	status, err := service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() initial error = %v", err)
	}
	if status.Revision != 1 {
		t.Fatalf("initial revision = %d, want 1", status.Revision)
	}

	if got := len(searchByPath(t, ctx, service, projectValue, "a.go")); got != 1 {
		t.Fatalf("initial search a.go chunks = %d, want 1", got)
	}

	writeFile(t, projectValue.Root.CanonicalPath, "c.go", `package main

func gamma() { _ = 3 }
`)
	status, err = service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() add error = %v", err)
	}
	if status.Revision != 2 {
		t.Fatalf("after add revision = %d, want 2", status.Revision)
	}
	if got := len(searchByPath(t, ctx, service, projectValue, "c.go")); got != 1 {
		t.Fatalf("after add search c.go chunks = %d, want 1", got)
	}

	manifestBefore, stateBefore := readCurrentManifestAndState(t, ctx, service, projectValue)
	if len(manifestBefore.Files) != 3 {
		t.Fatalf("manifest files before final changes = %d, want 3", len(manifestBefore.Files))
	}
	if stateBefore.Revision != 2 {
		t.Fatalf("manifest state revision before final changes = %d, want 2", stateBefore.Revision)
	}

	writeFile(t, projectValue.Root.CanonicalPath, "a.go", `package main

func alphaModified() { _ = 4 }
`)
	if err := os.Remove(filepath.Join(projectValue.Root.CanonicalPath, "b.go")); err != nil {
		t.Fatalf("remove b.go error = %v", err)
	}
	if err := os.Rename(filepath.Join(projectValue.Root.CanonicalPath, "c.go"), filepath.Join(projectValue.Root.CanonicalPath, "renamed.go")); err != nil {
		t.Fatalf("rename c.go error = %v", err)
	}

	status, err = service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() modify/delete/rename error = %v", err)
	}
	if status.Revision != 3 {
		t.Fatalf("after modify/delete/rename revision = %d, want 3", status.Revision)
	}

	if len(searchByPath(t, ctx, service, projectValue, "b.go")) != 0 {
		t.Fatal("expected deleted b.go to be absent")
	}
	if len(searchByPath(t, ctx, service, projectValue, "c.go")) != 0 {
		t.Fatal("expected missing c.go to be absent after rename")
	}
	if len(searchByPath(t, ctx, service, projectValue, "renamed.go")) != 1 {
		t.Fatal("expected renamed.go to be present after rename")
	}

	manifestAfter, stateAfter := readCurrentManifestAndState(t, ctx, service, projectValue)
	if len(manifestAfter.Files) != 2 {
		t.Fatalf("manifest files after final changes = %d, want 2", len(manifestAfter.Files))
	}
	if stateAfter.Revision != 3 {
		t.Fatalf("manifest state revision after final changes = %d, want 3", stateAfter.Revision)
	}
	if hasManifestPath(manifestAfter, "b.go") || hasManifestPath(manifestAfter, "c.go") || !hasManifestPath(manifestAfter, "renamed.go") {
		t.Fatalf("manifest paths after final changes = %#v", manifestAfter.Files)
	}

	status, err = service.EnsureFresh(ctx, projectValue)
	if err != nil {
		t.Fatalf("EnsureFresh() no-op error = %v", err)
	}
	if status.Revision != 3 {
		t.Fatalf("after no-op revision = %d, want 3", status.Revision)
	}
}

func TestEnsureFreshConcurrentCallsAreCoalescedToSingleRefresh(t *testing.T) {
	ctx := context.Background()
	service := NewService(Config{IndexRoot: t.TempDir()})
	projectValue := newProject(t, map[string]string{
		"main.go": `package main

func concurrentToken() {}
`,
	})

	if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
		t.Fatalf("EnsureFresh() initial error = %v", err)
	}

	writeFile(t, projectValue.Root.CanonicalPath, "main.go", `package main

func concurrentTokenMutated() {}
`)

	var discoverCalls int64
	service.discover = func(callCtx context.Context, root string, previous discovery.Previous) (discovery.Result, error) {
		atomic.AddInt64(&discoverCalls, 1)
		return discovery.DiscoverWithPrevious(callCtx, root, previous)
	}

	var wg sync.WaitGroup
	const concurrentCalls = 8
	errCh := make(chan error, concurrentCalls)
	statusCh := make(chan Status, concurrentCalls)
	start := make(chan struct{})

	for range concurrentCalls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			status, err := service.EnsureFresh(ctx, projectValue)
			if err != nil {
				errCh <- err
				return
			}
			statusCh <- status
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	close(statusCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent EnsureFresh() error = %v", err)
		}
	}

	for status := range statusCh {
		if status.State != sidecar.StateReady {
			t.Fatalf("concurrent status state = %q, want %q", status.State, sidecar.StateReady)
		}
		if status.Revision != 2 {
			t.Fatalf("concurrent revision = %d, want 2", status.Revision)
		}
	}

	if got := atomic.LoadInt64(&discoverCalls); got != 2 {
		t.Fatalf("discover calls = %d, want 2", got)
	}
}

func TestEnsureFreshSourceMutationDuringRefreshIsBounded(t *testing.T) {
	t.Run("transient mutation converges", func(t *testing.T) {
		ctx := context.Background()
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{
			"main.go": `package main

func sourceStable() {}
`,
		})
		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() initial error = %v", err)
		}

		root := projectValue.Root.CanonicalPath
		var calls int32
		service.discover = func(callCtx context.Context, path string, previous discovery.Previous) (discovery.Result, error) {
			callNumber := atomic.AddInt32(&calls, 1)
			result, err := discovery.DiscoverWithPrevious(callCtx, path, nil)
			if err != nil {
				return result, err
			}
			if callNumber == 1 {
				writeFile(t, root, "main.go", `package main

func sourceConverged() {}
`)
				for index := range result.Files {
					if result.Files[index].Path != "main.go" {
						continue
					}
					result.Files[index].ContentHash = "transient-mismatch-hash"
				}
			}
			return result, nil
		}

		status, err := service.EnsureFresh(ctx, projectValue)
		if err != nil {
			t.Fatalf("EnsureFresh() transient mutation error = %v", err)
		}
		if status.State != sidecar.StateReady {
			t.Fatalf("transient mutation state = %q, want %q", status.State, sidecar.StateReady)
		}
		if status.Revision != 2 {
			t.Fatalf("transient mutation revision = %d, want 2", status.Revision)
		}
		if got := atomic.LoadInt32(&calls); got != 3 {
			t.Fatalf("transient discovery calls = %d, want 3", got)
		}
		if len(searchByPath(t, ctx, service, projectValue, "main.go")) != 1 {
			t.Fatal("expected main.go chunks after convergence")
		}
	})

	t.Run("persistent mutation becomes stale", func(t *testing.T) {
		ctx := context.Background()
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{
			"main.go": `package main

func sourceStable() {}
`,
		})
		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() initial error = %v", err)
		}
		root := projectValue.Root.CanonicalPath
		var calls int32
		service.discover = func(callCtx context.Context, path string, previous discovery.Previous) (discovery.Result, error) {
			callNumber := atomic.AddInt32(&calls, 1)
			result, err := discovery.DiscoverWithPrevious(callCtx, path, nil)
			if err != nil {
				return result, err
			}
			switch callNumber {
			case 1:
				writeFile(t, root, "main.go", `package main

func sourceMutating1() {}
`)
				for index := range result.Files {
					if result.Files[index].Path != "main.go" {
						continue
					}
					result.Files[index].ContentHash = "persistent-mismatch-hash-1"
				}
			case 2:
				writeFile(t, root, "main.go", `package main

func sourceMutating2() {}
`)
				for index := range result.Files {
					if result.Files[index].Path != "main.go" {
						continue
					}
					result.Files[index].ContentHash = "persistent-mismatch-hash-2"
				}
			}
			return result, nil
		}

		status, err := service.EnsureFresh(ctx, projectValue)
		var indexingErr *Error
		if !errors.As(err, &indexingErr) {
			t.Fatalf("EnsureFresh() persistent mutation error = %v", err)
		}
		if status.State != sidecar.StateStale {
			t.Fatalf("persistent mutation state = %q, want %q", status.State, sidecar.StateStale)
		}
		if status.Revision != 1 {
			t.Fatalf("persistent mutation revision = %d, want 1", status.Revision)
		}
		if got := atomic.LoadInt32(&calls); got != 2 {
			t.Fatalf("persistent discovery calls = %d, want 2", got)
		}
	})
}

func TestEnsureFreshRecoversFromSidecarTampering(t *testing.T) {
	t.Run("updating state triggers rebuild", func(t *testing.T) {
		ctx := context.Background()
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{
			"main.go": `package main

func recoverable() {}
`,
		})

		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() initial error = %v", err)
		}

		oldState := mustReadState(t, ctx, service, projectValue)
		if err := markCurrentGenerationUpdating(t, ctx, service, projectValue, oldState.Revision+1); err != nil {
			t.Fatalf("mark updating error = %v", err)
		}

		status, err := service.EnsureFresh(ctx, projectValue)
		if err != nil {
			t.Fatalf("EnsureFresh() recovery error = %v", err)
		}
		if status.State != sidecar.StateReady {
			t.Fatalf("status state = %q, want %q", status.State, sidecar.StateReady)
		}
		if status.EpochID == oldState.Epoch {
			t.Fatalf("rebuild did not change epoch: old %s new %s", oldState.Epoch, status.EpochID)
		}
		assertOnlyCurrentGeneration(t, service, projectValue, status.EpochID)
	})

	t.Run("corrupt search marker triggers rebuild", func(t *testing.T) {
		ctx := context.Background()
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{
			"main.go": `package main

func recoverable() {}
`,
		})

		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() initial error = %v", err)
		}

		oldState := mustReadState(t, ctx, service, projectValue)
		if err := removeSearchIndexForCurrentGeneration(t, ctx, service, projectValue); err != nil {
			t.Fatalf("tamper search index error = %v", err)
		}

		status, err := service.EnsureFresh(ctx, projectValue)
		if err != nil {
			t.Fatalf("EnsureFresh() recovery error = %v", err)
		}
		if status.State != sidecar.StateReady {
			t.Fatalf("status state = %q, want %q", status.State, sidecar.StateReady)
		}
		if status.EpochID == oldState.Epoch {
			t.Fatalf("rebuild did not change epoch: old %s new %s", oldState.Epoch, status.EpochID)
		}
		assertOnlyCurrentGeneration(t, service, projectValue, status.EpochID)
	})
}

func TestIndexingErrorsAreReturnedAsIndexingError(t *testing.T) {
	ctx := context.Background()

	t.Run("discover failure", func(t *testing.T) {
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{"main.go": `package main

func fail() {}`})
		service.discover = func(_ context.Context, _ string, _ discovery.Previous) (discovery.Result, error) {
			return discovery.Result{}, fmt.Errorf("discover failed")
		}

		status, err := service.EnsureFresh(ctx, projectValue)
		if err == nil {
			t.Fatal("expected EnsureFresh() error")
		}
		var indexingErr *Error
		if !errors.As(err, &indexingErr) {
			t.Fatalf("expected *indexing.Error, got %T", err)
		}
		if status.State != sidecar.StateFailed {
			t.Fatalf("status state = %q, want %q", status.State, sidecar.StateFailed)
		}
		persisted, statusErr := service.Status(ctx, projectValue)
		if statusErr != nil {
			t.Fatalf("Status() after failure error = %v", statusErr)
		}
		if persisted.State != sidecar.StateFailed || len(persisted.Diagnostics) == 0 {
			t.Fatalf("persisted failed status = %+v", persisted)
		}

		service.discover = discovery.DiscoverWithPrevious
		recovered, recoveryErr := service.EnsureFresh(ctx, projectValue)
		if recoveryErr != nil {
			t.Fatalf("EnsureFresh() recovery error = %v", recoveryErr)
		}
		if recovered.State != sidecar.StateReady || recovered.EpochID == persisted.EpochID {
			t.Fatalf("recovered status = %+v, failed status = %+v", recovered, persisted)
		}
	})

	t.Run("build read failure", func(t *testing.T) {
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{"main.go": `package main

func fail() {}`})
		service.readFile = func(_ string) ([]byte, error) {
			return nil, errors.New("read failed")
		}

		status, err := service.EnsureFresh(ctx, projectValue)
		if err == nil {
			t.Fatal("expected EnsureFresh() error")
		}
		var indexingErr *Error
		if !errors.As(err, &indexingErr) {
			t.Fatalf("expected *indexing.Error, got %T", err)
		}
		if status.State != sidecar.StateFailed {
			t.Fatalf("status state = %q, want %q", status.State, sidecar.StateFailed)
		}
	})
}

func TestStatusDistinguishesLifecycleStates(t *testing.T) {
	ctx := context.Background()

	t.Run("updating", func(t *testing.T) {
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{"main.go": `package main

func statusUpdating() {}
`})
		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		state := mustReadState(t, ctx, service, projectValue)
		if err := markCurrentGenerationUpdating(t, ctx, service, projectValue, state.Revision+1); err != nil {
			t.Fatalf("mark updating error = %v", err)
		}

		status, err := service.Status(ctx, projectValue)
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if status.State != sidecar.StateUpdating {
			t.Fatalf("Status() state = %q, want %q", status.State, sidecar.StateUpdating)
		}
	})

	t.Run("stale", func(t *testing.T) {
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{"main.go": `package main

func statusStale() {}
`})
		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if err := markCurrentGenerationStatus(t, ctx, service, projectValue, sidecar.StateStale); err != nil {
			t.Fatalf("mark stale error = %v", err)
		}

		status, err := service.Status(ctx, projectValue)
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if status.State != sidecar.StateStale {
			t.Fatalf("Status() state = %q, want %q", status.State, sidecar.StateStale)
		}
	})

	t.Run("degraded", func(t *testing.T) {
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{"main.go": `package main

func statusDegraded() {}
`})
		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if err := markCurrentGenerationDegraded(t, ctx, service, projectValue); err != nil {
			t.Fatalf("mark degraded error = %v", err)
		}

		status, err := service.Status(ctx, projectValue)
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if status.State != sidecar.StateDegraded {
			t.Fatalf("Status() state = %q, want %q", status.State, sidecar.StateDegraded)
		}
	})

	t.Run("failed", func(t *testing.T) {
		service := NewService(Config{IndexRoot: t.TempDir()})
		projectValue := newProject(t, map[string]string{"main.go": `package main

func statusFailed() {}
`})
		if _, err := service.EnsureFresh(ctx, projectValue); err != nil {
			t.Fatalf("EnsureFresh() error = %v", err)
		}
		if err := markCurrentGenerationStatus(t, ctx, service, projectValue, sidecar.StateFailed); err != nil {
			t.Fatalf("mark failed error = %v", err)
		}

		status, err := service.Status(ctx, projectValue)
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if status.State != sidecar.StateFailed {
			t.Fatalf("Status() state = %q, want %q", status.State, sidecar.StateFailed)
		}
	})
}

func newProject(t *testing.T, files map[string]string) project.Project {
	t.Helper()

	projectID := uuid.NewString()
	root := t.TempDir()
	resolved, err := project.Canonicalize(root)
	if err != nil {
		t.Fatalf("canonicalize(%q) error = %v", root, err)
	}

	value := project.Project{
		ID:   projectID,
		Root: &resolved,
	}
	for path, content := range files {
		writeFile(t, root, path, content)
	}

	return value
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()

	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", fullPath, err)
	}
}

func readCurrentManifestAndState(t *testing.T, ctx context.Context, service *Service, value project.Project) (manifest.Manifest, sidecar.State) {
	t.Helper()

	indexedProject, err := sidecar.Open(ctx, sidecar.OpenConfig{
		ProjectID:     value.ID,
		CanonicalRoot: value.Root.CanonicalPath,
		IndexRoot:     service.config.IndexRoot,
	})
	if err != nil {
		t.Fatalf("sidecar.Open() error = %v", err)
	}
	defer indexedProject.Close()

	generation, err := indexedProject.Current(ctx)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	defer generation.Close()

	valueManifest, err := generation.Manifest()
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	valueState, err := generation.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}

	return valueManifest, valueState
}

func mustReadState(t *testing.T, ctx context.Context, service *Service, value project.Project) sidecar.State {
	t.Helper()

	_, state := readCurrentManifestAndState(t, ctx, service, value)
	return state
}

func searchByPath(t *testing.T, ctx context.Context, service *Service, value project.Project, path string) []retrieval.SearchResult {
	t.Helper()

	results, _, err := service.Search(ctx, value, retrieval.SearchRequest{Query: path})
	if err != nil {
		t.Fatalf("Search(%q) error = %v", path, err)
	}
	matched := make([]retrieval.SearchResult, 0, len(results))
	for _, result := range results {
		if result.Path == path {
			matched = append(matched, result)
		}
	}
	return matched
}

func markCurrentGenerationUpdating(t *testing.T, ctx context.Context, service *Service, value project.Project, targetRevision int64) error {
	t.Helper()

	projectHandle, err := sidecar.Open(ctx, sidecar.OpenConfig{
		ProjectID:     value.ID,
		CanonicalRoot: value.Root.CanonicalPath,
		IndexRoot:     service.config.IndexRoot,
	})
	if err != nil {
		t.Fatalf("sidecar.Open() error = %v", err)
	}
	defer projectHandle.Close()

	generation, err := projectHandle.CurrentForUpdate(ctx)
	if err != nil {
		return err
	}
	defer generation.Close()

	if err := generation.BeginUpdate(targetRevision); err != nil {
		return err
	}
	return nil
}

func markCurrentGenerationStatus(t *testing.T, ctx context.Context, service *Service, value project.Project, status sidecar.StateStatus) error {
	t.Helper()

	projectHandle, err := sidecar.Open(ctx, sidecar.OpenConfig{
		ProjectID:     value.ID,
		CanonicalRoot: value.Root.CanonicalPath,
		IndexRoot:     service.config.IndexRoot,
	})
	if err != nil {
		t.Fatalf("sidecar.Open() error = %v", err)
	}
	defer projectHandle.Close()

	generation, err := projectHandle.CurrentForUpdate(ctx)
	if err != nil {
		return err
	}
	defer generation.Close()

	return generation.MarkStatus(status, []string{"test-fixture"})
}

func markCurrentGenerationDegraded(t *testing.T, ctx context.Context, service *Service, value project.Project) error {
	t.Helper()

	projectHandle, err := sidecar.Open(ctx, sidecar.OpenConfig{
		ProjectID:     value.ID,
		CanonicalRoot: value.Root.CanonicalPath,
		IndexRoot:     service.config.IndexRoot,
	})
	if err != nil {
		return err
	}
	defer projectHandle.Close()

	generation, err := projectHandle.CurrentForUpdate(ctx)
	if err != nil {
		return err
	}
	defer generation.Close()

	state, err := generation.State()
	if err != nil {
		return err
	}
	valueManifest, err := generation.Manifest()
	if err != nil {
		return err
	}
	store, err := indexstore.Open(generation.SearchPath(), value.ID, generation.Epoch(), state.Revision)
	if err != nil {
		return err
	}
	defer store.Close()

	targetRevision := state.Revision + 1
	if err := generation.BeginUpdate(targetRevision); err != nil {
		return err
	}
	if err := generation.CommitUpdate(valueManifest, targetRevision, sidecar.StateDegraded); err != nil {
		return err
	}
	if err := store.Apply(retrieval.ApplyRequest{
		ProjectID:     value.ID,
		EpochID:       generation.Epoch(),
		IndexRevision: targetRevision,
	}); err != nil {
		return err
	}

	return generation.CompleteUpdate(targetRevision, sidecar.StateDegraded, []string{"test-fixture"})
}

func removeSearchIndexForCurrentGeneration(t *testing.T, ctx context.Context, service *Service, value project.Project) error {
	t.Helper()

	projectHandle, err := sidecar.Open(ctx, sidecar.OpenConfig{
		ProjectID:     value.ID,
		CanonicalRoot: value.Root.CanonicalPath,
		IndexRoot:     service.config.IndexRoot,
	})
	if err != nil {
		t.Fatalf("sidecar.Open() error = %v", err)
	}
	defer projectHandle.Close()

	generation, err := projectHandle.Current(ctx)
	if err != nil {
		return err
	}
	defer generation.Close()

	return os.RemoveAll(generation.SearchPath())
}

func hasManifestPath(value manifest.Manifest, path string) bool {
	for _, item := range value.Files {
		if item.Path == path {
			return true
		}
	}

	return false
}

func assertOnlyCurrentGeneration(t *testing.T, service *Service, value project.Project, epoch string) {
	t.Helper()

	root := filepath.Join(service.config.IndexRoot, value.ID, "generations")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read generations: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != epoch {
		t.Fatalf("generations = %+v, want only %q", entries, epoch)
	}
}

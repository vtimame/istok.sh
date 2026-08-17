package indexing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sync/singleflight"

	"s26.dev/istok-cli/internal/indexing/discovery"
	"s26.dev/istok-cli/internal/indexing/sidecar"
	"s26.dev/istok-cli/internal/indexstore"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/retrieval"
)

type discoverFunc func(context.Context, string, discovery.Previous) (discovery.Result, error)

type Service struct {
	config   Config
	group    singleflight.Group
	locks    sync.Map
	discover discoverFunc
	readFile func(string) ([]byte, error)
}

func NewService(config Config) *Service {
	return &Service{
		config:   config,
		discover: discovery.DiscoverWithPrevious,
		readFile: os.ReadFile,
	}
}

func (s *Service) EnsureFresh(ctx context.Context, value project.Project) (Status, error) {
	if err := validateProject(value); err != nil {
		status := failedStatus(err)
		return status, wrapError(status, err)
	}

	projectKey := value.ID + "\x00" + value.Root.CanonicalPath
	result := s.group.DoChan("fresh\x00"+projectKey, func() (any, error) {
		return s.withProjectLock(projectKey, func() (Status, error) {
			return s.ensureFresh(ctx, value)
		})
	})

	select {
	case <-ctx.Done():
		status := failedStatus(ctx.Err())
		return status, wrapError(status, ctx.Err())
	case call := <-result:
		if call.Val == nil {
			status := failedStatus(call.Err)
			return status, wrapError(status, call.Err)
		}

		status := call.Val.(Status)
		return status, call.Err
	}
}

func (s *Service) withProjectLock(key string, operation func() (Status, error)) (Status, error) {
	value, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	return operation()
}

func (s *Service) Search(ctx context.Context, value project.Project, request retrieval.SearchRequest) ([]retrieval.SearchResult, Status, error) {
	if request.Limit < 0 || request.Limit > maxSearchLimit {
		err := fmt.Errorf("search limit must be between 0 and %d", maxSearchLimit)
		status := failedStatus(err)

		return nil, status, wrapError(status, err)
	}

	status, err := s.EnsureFresh(ctx, value)
	if err != nil {
		return nil, status, err
	}
	if status.State != sidecar.StateReady && status.State != sidecar.StateDegraded {
		err := fmt.Errorf("index is not readable in state %q", status.State)
		return nil, status, wrapError(status, err)
	}

	indexedProject, err := s.openProject(ctx, value)
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	defer indexedProject.Close()

	generation, err := indexedProject.Current(ctx)
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	defer generation.Close()

	state, err := generation.State()
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	status = statusFromState(state)
	if status.State != sidecar.StateReady && status.State != sidecar.StateDegraded {
		err := fmt.Errorf("index became unreadable in state %q", status.State)
		return nil, status, wrapError(status, err)
	}

	store, err := indexstore.Open(generation.SearchPath(), value.ID, generation.Epoch(), state.Revision)
	if err != nil {
		status.State = sidecar.StateFailed
		status.Diagnostics = []string{err.Error()}
		return nil, status, wrapError(status, err)
	}
	defer store.Close()

	results, err := store.Search(request)
	if err != nil {
		return nil, status, wrapError(status, err)
	}

	return results, status, nil
}

// SearchTask builds the bounded local code context included in task claims.
// It intentionally has no transport, network, or task lifecycle dependency.
func (s *Service) SearchTask(ctx context.Context, value project.Project, request retrieval.TaskSearchRequest) ([]retrieval.SearchResult, Status, error) {
	status, err := s.EnsureFresh(ctx, value)
	if err != nil {
		return nil, status, err
	}
	if status.State != sidecar.StateReady && status.State != sidecar.StateDegraded {
		err := fmt.Errorf("index is not readable in state %q", status.State)
		return nil, status, wrapError(status, err)
	}

	indexedProject, err := s.openProject(ctx, value)
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	defer indexedProject.Close()

	generation, err := indexedProject.Current(ctx)
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	defer generation.Close()

	state, err := generation.State()
	if err != nil {
		status := failedStatus(err)
		return nil, status, wrapError(status, err)
	}
	status = statusFromState(state)
	if status.State != sidecar.StateReady && status.State != sidecar.StateDegraded {
		err := fmt.Errorf("index became unreadable in state %q", status.State)
		return nil, status, wrapError(status, err)
	}

	store, err := indexstore.Open(generation.SearchPath(), value.ID, generation.Epoch(), state.Revision)
	if err != nil {
		status.State = sidecar.StateFailed
		status.Diagnostics = []string{err.Error()}
		return nil, status, wrapError(status, err)
	}
	defer store.Close()

	results, err := retrieval.SearchTask(ctx, store, graphLookup{repository: generation.Graph()}, request)
	if err != nil {
		return nil, status, wrapError(status, err)
	}

	return results, status, nil
}

func (s *Service) Status(ctx context.Context, value project.Project) (Status, error) {
	if err := validateProject(value); err != nil {
		status := failedStatus(err)
		return status, wrapError(status, err)
	}

	indexedProject, err := s.openProject(ctx, value)
	if err != nil {
		status := failedStatus(err)
		return status, wrapError(status, err)
	}
	defer indexedProject.Close()

	state, err := indexedProject.InspectState(ctx)
	if errors.Is(err, sidecar.ErrMissing) {
		return Status{State: sidecar.StateNeverIndexed}, nil
	}
	if err != nil {
		status := failedStatus(err)
		return status, wrapError(status, err)
	}

	status := statusFromState(state)
	if state.Status == sidecar.StateUpdating || state.Status == sidecar.StateFailed {
		return status, nil
	}

	generation, err := indexedProject.Current(ctx)
	if err != nil {
		status.State = sidecar.StateFailed
		status.Diagnostics = append(status.Diagnostics, err.Error())
		return status, wrapError(status, err)
	}
	defer generation.Close()

	store, err := indexstore.Open(generation.SearchPath(), value.ID, generation.Epoch(), state.Revision)
	if err != nil {
		status.State = sidecar.StateFailed
		status.Diagnostics = append(status.Diagnostics, err.Error())
		return status, wrapError(status, err)
	}
	defer store.Close()

	return status, nil
}

func (s *Service) openProject(ctx context.Context, value project.Project) (*sidecar.Project, error) {
	return sidecar.Open(ctx, sidecar.OpenConfig{
		ProjectID:     value.ID,
		CanonicalRoot: value.Root.CanonicalPath,
		IndexRoot:     s.config.IndexRoot,
	})
}

func validateProject(value project.Project) error {
	if value.ID == "" {
		return errors.New("project ID is required")
	}
	if value.Root == nil || value.Root.CanonicalPath == "" {
		return errors.New("project root is required")
	}
	if value.DeletedAt != nil {
		return errors.New("deleted project cannot be indexed")
	}

	return nil
}

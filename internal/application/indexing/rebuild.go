package indexing

import (
	"context"
	"fmt"

	"s26.dev/istok-cli/internal/indexing/sidecar"
	"s26.dev/istok-cli/internal/project"
)

// Rebuild creates and publishes a complete replacement generation, even when
// the current index is already fresh. A failed replacement never displaces the
// current readable generation.
func (s *Service) Rebuild(ctx context.Context, value project.Project) (Status, error) {
	if err := validateProject(value); err != nil {
		status := failedStatus(err)
		return status, wrapError(status, err)
	}

	projectKey := value.ID + "\x00" + value.Root.CanonicalPath
	result := s.group.DoChan("rebuild\x00"+projectKey, func() (any, error) {
		return s.withProjectLock(projectKey, func() (Status, error) {
			return s.rebuildForced(ctx, value)
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

		return call.Val.(Status), call.Err
	}
}

func (s *Service) rebuildForced(ctx context.Context, value project.Project) (Status, error) {
	indexedProject, err := s.openProject(ctx, value)
	if err != nil {
		status := failedStatus(err)
		return status, wrapError(status, err)
	}
	defer indexedProject.Close()

	active, err := s.rebuild(ctx, indexedProject, value)
	if err != nil {
		status := s.currentStatus(ctx, indexedProject, err)
		return status, wrapError(status, err)
	}
	defer active.close()

	if err := indexedProject.CleanupGenerations(active.generation.Epoch()); err != nil {
		status := statusFromState(active.state)
		return status, wrapError(status, fmt.Errorf("cleanup old index generations: %w", err))
	}

	return statusFromState(active.state), nil
}

func (s *Service) currentStatus(ctx context.Context, indexedProject *sidecar.Project, fallback error) Status {
	generation, err := indexedProject.Current(ctx)
	if err != nil {
		return failedStatus(fallback)
	}
	defer generation.Close()

	state, err := generation.State()
	if err != nil {
		return failedStatus(fallback)
	}

	return statusFromState(state)
}

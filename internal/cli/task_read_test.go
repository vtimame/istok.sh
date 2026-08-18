package cli

import (
	"context"
	"testing"

	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/task"
)

func TestTaskReadResolverShowResolvesProjectAndSelectsByProjectScopedNumber(t *testing.T) {
	projectID := mustTaskIDFromTaskLib(t)
	projectService := &fakeTaskProjectResolver{
		current: project.Project{ID: projectID},
	}
	taskService := &fakeTaskReadService{
		showResult: task.Show{Task: task.Task{ID: "task-id", ProjectID: projectID}},
	}
	resolver := newTaskReadResolver(projectService, taskService)

	_, err := resolver.Show(context.Background(), "/tmp/cwd", TaskShowCommand{ID: 3})
	if err != nil {
		t.Fatalf("Show() = %v", err)
	}

	if projectService.cwds != "/tmp/cwd" {
		t.Fatalf("Show() called with cwd %q", projectService.cwds)
	}
	if taskService.showSelector.ProjectID != projectID {
		t.Fatalf("Show() selector project id = %q, want %q", taskService.showSelector.ProjectID, projectID)
	}
	if taskService.showSelector.Number != 3 {
		t.Fatalf("Show() selector number = %d, want %d", taskService.showSelector.Number, 3)
	}
	if taskService.showDeleted {
		t.Fatalf("Show() requested includeDeleted=%t, want false", taskService.showDeleted)
	}
}

type fakeTaskProjectResolver struct {
	current project.Project
	err     error
	cwds    string
}

func (s *fakeTaskProjectResolver) Current(_ context.Context, cwd string) (project.Project, error) {
	s.cwds = cwd
	return s.current, s.err
}

type fakeTaskReadService struct {
	showResult    task.Show
	showSelector  task.Selector
	showDeleted   bool
	showCalled    bool
	showErr       error
	listCalled    bool
	listProjectID string
	listOptions   task.ListOptions
}

func (s *fakeTaskReadService) List(_ context.Context, projectID string, options task.ListOptions) ([]task.TaskListItem, error) {
	s.listCalled = true
	s.listProjectID = projectID
	s.listOptions = options
	return nil, nil
}

func (s *fakeTaskReadService) Show(_ context.Context, selector task.Selector, includeDeleted bool) (task.Show, error) {
	s.showCalled = true
	s.showSelector = selector
	s.showDeleted = includeDeleted
	return s.showResult, s.showErr
}

func mustTaskIDFromTaskLib(t *testing.T) string {
	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}

	return id
}

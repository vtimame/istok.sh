package taskapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"s26.dev/istok-cli/internal/task"
)

type fakeRepository struct {
	value      task.Task
	values     map[string]task.Task
	deleteCall bool
	addCall    bool
}

func (r *fakeRepository) unexpected() { panic("unexpected repository call") }
func (r *fakeRepository) Create(context.Context, task.CreateInput, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) Update(context.Context, string, int64, task.Patch, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) Comment(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) Progress(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) Block(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) Unblock(context.Context, string, int64, string, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) Restore(context.Context, string, int64, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) AddDependency(context.Context, string, string, int64, task.ActorSnapshot) (task.Task, error) {
	r.addCall = true
	return r.value, nil
}
func (r *fakeRepository) RemoveDependency(context.Context, string, string, int64, task.ActorSnapshot) (task.Task, error) {
	r.unexpected()
	return task.Task{}, nil
}
func (r *fakeRepository) List(context.Context, string, task.ListOptions) ([]task.TaskListItem, error) {
	r.unexpected()
	return nil, nil
}
func (r *fakeRepository) Show(context.Context, task.Selector, bool) (task.Show, error) {
	r.unexpected()
	return task.Show{}, nil
}
func (r *fakeRepository) Ready(context.Context, string) ([]task.TaskListItem, error) {
	r.unexpected()
	return nil, nil
}

func (r *fakeRepository) Resolve(_ context.Context, selector task.Selector, _ bool) (task.Task, error) {
	if r.values != nil {
		return r.values[selector.ID], nil
	}

	return r.value, nil
}

func (r *fakeRepository) Delete(context.Context, string, int64, task.ActorSnapshot) (task.Task, error) {
	r.deleteCall = true
	return r.value, nil
}

type activeRuns struct{ active bool }

func (r activeRuns) HasActiveRun(context.Context, string) (bool, error) { return r.active, nil }

func TestDeleteRejectsActiveRunBeforeRepositoryMutation(t *testing.T) {
	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{value: task.Task{ID: id, ProjectID: projectID, Revision: 1, Status: task.StatusOpen}}
	service := NewService(repository, activeRuns{active: true})
	actor := task.ActorSnapshot{ID: "user", Kind: "user", Name: "User"}

	_, err = service.Delete(context.Background(), task.Selector{ProjectID: projectID, ID: id}, 1, actor)
	if task.ErrorCode(err) != task.CodeHasActiveRun {
		t.Fatalf("Delete() error = %v", err)
	}
	if repository.deleteCall {
		t.Fatal("repository Delete was called")
	}
}

func TestValidationRejectsBeforeRepositoryCalls(t *testing.T) {
	id := mustTaskID(t)
	projectID := mustTaskID(t)
	actor := task.ActorSnapshot{ID: "user", Kind: "user", Name: "User"}
	repository := &fakeRepository{value: task.Task{ID: id, ProjectID: projectID, Revision: 1, Status: task.StatusBlocked}}
	service := NewService(repository, NoActiveRuns{})

	if _, err := service.Show(context.Background(), task.Selector{}, false); task.ErrorCode(err) != task.CodeInvalid {
		t.Fatalf("Show() error = %v", err)
	}
	if _, err := service.Update(context.Background(), task.Selector{ProjectID: projectID, ID: id}, 1, task.Patch{}, actor); task.ErrorCode(err) != task.CodeInvalid {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := service.Block(context.Background(), task.Selector{ProjectID: projectID, ID: id}, 1, "reason", actor); task.ErrorCode(err) != task.CodeInvalidTransition {
		t.Fatalf("Block() error = %v", err)
	}
	if _, err := service.AddDependency(context.Background(), task.Selector{ProjectID: projectID, ID: id}, task.Selector{ProjectID: projectID, ID: id}, 1, actor); task.ErrorCode(err) != task.CodeInvalid {
		t.Fatalf("self dependency error = %v", err)
	}
}

func TestNoActiveRunsAllowsDelete(t *testing.T) {
	id := mustTaskID(t)
	projectID := mustTaskID(t)
	actor := task.ActorSnapshot{ID: "user", Kind: "user", Name: "User"}
	repository := &fakeRepository{value: task.Task{ID: id, ProjectID: projectID, Revision: 1, Status: task.StatusOpen}}

	if _, err := NewService(repository, NoActiveRuns{}).Delete(context.Background(), task.Selector{ProjectID: projectID, ID: id}, 1, actor); err != nil {
		t.Fatal(err)
	}
	if !repository.deleteCall {
		t.Fatal("repository Delete was not called")
	}
}

func TestDeleteStopsWhenRunInspectorFails(t *testing.T) {
	id := mustTaskID(t)
	projectID := mustTaskID(t)
	repository := &fakeRepository{value: task.Task{ID: id, ProjectID: projectID, Revision: 1, Status: task.StatusOpen}}
	inspectErr := errors.New("inspector unavailable")
	service := NewService(repository, runInspectorError{err: inspectErr})
	actor := task.ActorSnapshot{ID: "user", Kind: "user", Name: "User"}

	_, err := service.Delete(context.Background(), task.Selector{ProjectID: projectID, ID: id}, 1, actor)
	if !errors.Is(err, inspectErr) || repository.deleteCall {
		t.Fatalf("Delete() error=%v deleteCall=%v", err, repository.deleteCall)
	}
}

type runInspectorError struct{ err error }

func (r runInspectorError) HasActiveRun(context.Context, string) (bool, error) { return false, r.err }

func TestDependencyPolicyRejectsCrossProjectAndDeletedBeforePort(t *testing.T) {
	firstID := mustTaskID(t)
	secondID := mustTaskID(t)
	projectID := mustTaskID(t)
	otherProjectID := mustTaskID(t)
	actor := task.ActorSnapshot{ID: "user", Kind: "user", Name: "User"}
	deletedAt := time.Now()

	for _, values := range []map[string]task.Task{
		{firstID: {ID: firstID, ProjectID: projectID}, secondID: {ID: secondID, ProjectID: otherProjectID}},
		{firstID: {ID: firstID, ProjectID: projectID, DeletedAt: &deletedAt}, secondID: {ID: secondID, ProjectID: projectID}},
	} {
		repository := &fakeRepository{values: values}
		service := NewService(repository, NoActiveRuns{})

		_, err := service.AddDependency(context.Background(), task.Selector{ProjectID: projectID, ID: firstID}, task.Selector{ProjectID: projectID, ID: secondID}, 1, actor)
		if task.ErrorCode(err) != task.CodeConflict || repository.addCall {
			t.Fatalf("AddDependency() error=%v addCall=%v", err, repository.addCall)
		}
	}
}

func mustTaskID(t *testing.T) string {
	t.Helper()

	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}

	return id
}

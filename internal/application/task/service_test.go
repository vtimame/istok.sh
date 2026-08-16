package taskapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"s26.dev/istok-cli/internal/task"
)

type fakeRepository struct {
	value        task.Task
	createInput  task.CreateInput
	createCall   bool
	values       map[string]task.Task
	deleteCall   bool
	addCall      bool
	listValues   []task.TaskListItem
	readyValues  []task.TaskListItem
	readyErr     error
	showValue    task.Show
	showErr      error
	showCalled   bool
	showSelector task.Selector
	showDeleted  bool
}

func (r *fakeRepository) unexpected() { panic("unexpected repository call") }
func (r *fakeRepository) Create(_ context.Context, input task.CreateInput, _ task.ActorSnapshot) (task.Task, error) {
	r.createInput = input
	r.createCall = true
	return r.value, nil
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
	if r.listValues != nil {
		return r.listValues, nil
	}

	r.unexpected()
	return nil, nil
}
func (r *fakeRepository) Show(_ context.Context, selector task.Selector, includeDeleted bool) (task.Show, error) {
	r.showCalled = true
	r.showSelector = selector
	r.showDeleted = includeDeleted
	return r.showValue, r.showErr
}
func (r *fakeRepository) Ready(context.Context, string) ([]task.TaskListItem, error) {
	if r.readyValues != nil || r.readyErr != nil {
		return r.readyValues, r.readyErr
	}

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

type activeRunsByID map[string]bool

func (r activeRunsByID) HasActiveRun(_ context.Context, id string) (bool, error) {
	return r[id], nil
}

func TestCreateGeneratesUUIDAtApplicationBoundary(t *testing.T) {
	projectID := mustProjectID(t)
	repository := &fakeRepository{value: task.Task{ProjectID: projectID}}
	service := NewService(repository, NoActiveRuns{})

	if _, err := service.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "Task"}, task.ActorSnapshot{ID: "cli", Kind: "cli", Name: "CLI"}); err != nil {
		t.Fatal(err)
	}
	if !repository.createCall || !task.IsUUIDv7(repository.createInput.ID) {
		t.Fatalf("Create() input = %#v", repository.createInput)
	}
}

func TestListSetsActiveRunStateOnTaskListItems(t *testing.T) {
	first := mustTaskID(t)
	second := mustTaskID(t)
	repository := &fakeRepository{
		listValues: []task.TaskListItem{
			{Task: task.Task{ID: first}},
			{Task: task.Task{ID: second}},
		},
	}
	service := NewService(repository, activeRunsByID{first: true})
	list, err := service.List(context.Background(), mustProjectID(t), task.ListOptions{})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if !list[0].HasActiveRun || list[1].HasActiveRun {
		t.Fatalf("List() = %#v", list)
	}
}

func TestListReturnsActiveRunInspectorErrors(t *testing.T) {
	taskID := mustTaskID(t)
	repository := &fakeRepository{
		listValues: []task.TaskListItem{
			{Task: task.Task{ID: taskID}},
		},
	}
	inspectErr := errors.New("inspector unavailable")
	service := NewService(repository, runInspectorError{err: inspectErr})
	_, err := service.List(context.Background(), mustProjectID(t), task.ListOptions{})
	if !errors.Is(err, inspectErr) {
		t.Fatalf("List() error = %v", err)
	}
	if !strings.Contains(err.Error(), taskID) {
		t.Fatalf("List() error = %v", err)
	}
}

func TestReadyExcludesTasksWithActiveRuns(t *testing.T) {
	first := mustTaskID(t)
	second := mustTaskID(t)
	repository := &fakeRepository{
		readyValues: []task.TaskListItem{
			{Task: task.Task{ID: first}},
			{Task: task.Task{ID: second}},
		},
	}
	service := NewService(repository, activeRunsByID{first: true})

	ready, err := service.Ready(context.Background(), mustProjectID(t))
	if err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	if len(ready) != 1 || ready[0].ID != second {
		t.Fatalf("Ready() = %#v", ready)
	}
}

func TestReadyReturnsActiveRunInspectorErrors(t *testing.T) {
	taskID := mustTaskID(t)
	inspectErr := errors.New("inspector unavailable")
	repository := &fakeRepository{
		readyValues: []task.TaskListItem{{Task: task.Task{ID: taskID}}},
	}
	service := NewService(repository, runInspectorError{err: inspectErr})

	_, err := service.Ready(context.Background(), mustProjectID(t))
	if !errors.Is(err, inspectErr) {
		t.Fatalf("Ready() error = %v", err)
	}
	if !strings.Contains(err.Error(), taskID) {
		t.Fatalf("Ready() error = %v", err)
	}
}

func TestShowSetsActiveRunStateInResult(t *testing.T) {
	projectID := mustTaskID(t)
	taskID := mustTaskID(t)
	repository := &fakeRepository{
		showValue: task.Show{Task: task.Task{ID: taskID, ProjectID: projectID}},
	}
	service := NewService(repository, activeRunsByID{taskID: true})

	show, err := service.Show(context.Background(), task.Selector{ProjectID: projectID, ID: taskID}, false)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if !show.HasActiveRun {
		t.Fatalf("Show() = %#v", show)
	}
	if !repository.showCalled || repository.showSelector.ID != taskID || repository.showDeleted {
		t.Fatalf("Show() selector=%#v includeDeleted=%t called=%t", repository.showSelector, repository.showDeleted, repository.showCalled)
	}
}

func TestShowReturnsActiveRunInspectorErrorWithTaskID(t *testing.T) {
	projectID := mustTaskID(t)
	taskID := mustTaskID(t)
	inspectErr := errors.New("inspector unavailable")
	repository := &fakeRepository{showValue: task.Show{Task: task.Task{ID: taskID, ProjectID: projectID}}}
	service := NewService(repository, runInspectorError{err: inspectErr})

	_, err := service.Show(context.Background(), task.Selector{ProjectID: projectID, ID: taskID}, false)
	if !errors.Is(err, inspectErr) {
		t.Fatalf("Show() error = %v", err)
	}
	if !strings.Contains(err.Error(), taskID) {
		t.Fatalf("Show() error = %v", err)
	}
}

func mustProjectID(t *testing.T) string {
	t.Helper()

	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}

	return id
}

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

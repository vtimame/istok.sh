package taskrepo

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"s26.dev/istok-cli/internal/task"
)

func TestLifecycleDependencyAndReady(t *testing.T) {
	ctx := context.Background()
	repo, _, p := newTaskRepository(t)
	blocker, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "blocker"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "blocked"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	title := "renamed"
	updated, err := repo.Update(ctx, blocker.ID, blocker.Revision, task.Patch{Title: &title}, actor)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("Update()=%#v,%v", updated, err)
	}
	blocked, err = repo.AddDependency(ctx, updated.ID, blocked.ID, blocked.Revision, actor)
	if err != nil || blocked.Revision != 2 {
		t.Fatalf("AddDependency()=%#v,%v", blocked, err)
	}
	ready, err := repo.Ready(ctx, p.ID)
	if err != nil || len(ready) != 1 || ready[0].ID != updated.ID {
		t.Fatalf("Ready()=%#v,%v", ready, err)
	}
	listed, err := repo.List(ctx, p.ID, task.ListOptions{})
	if err != nil || len(listed) != 2 || len(listed[1].ActiveBlockers) != 1 {
		t.Fatalf("List()=%#v,%v", listed, err)
	}
	if _, err = repo.AddDependency(ctx, blocked.ID, updated.ID, updated.Revision, actor); task.ErrorCode(err) != task.CodeDependencyCycle {
		t.Fatalf("cycle error=%v", err)
	}
	removed, err := repo.RemoveDependency(ctx, updated.ID, blocked.ID, blocked.Revision, actor)
	if err != nil || removed.Revision != 3 {
		t.Fatalf("RemoveDependency()=%#v,%v", removed, err)
	}
	events, err := repo.Events(ctx, blocked.ID)
	if err != nil || len(events) != 3 || events[1].Type != "dependency_added" || events[2].Type != "dependency_removed" {
		t.Fatalf("Events()=%#v,%v", events, err)
	}
}

func TestConcurrentOppositeDependenciesProduceOneCycle(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "istok.db")
	projects := newProjects(t, database)
	p := initProject(t, projects, "graph")
	left := newRepository(t, database)
	right := newRepository(t, database)
	first, err := left.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "first"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := left.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "second"}, actor)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for _, call := range []func() error{
		func() error {
			_, err := left.AddDependency(ctx, first.ID, second.ID, second.Revision, actor)
			return err
		},
		func() error {
			_, err := right.AddDependency(ctx, second.ID, first.ID, first.Revision, actor)
			return err
		},
	} {
		group.Add(1)
		go func(fn func() error) {
			defer group.Done()
			<-start
			errs <- fn()
		}(call)
	}
	close(start)
	group.Wait()
	close(errs)

	var succeeded, cycles int
	for err := range errs {
		if err == nil {
			succeeded++
		} else if task.ErrorCode(err) == task.CodeDependencyCycle {
			cycles++
		} else {
			t.Fatalf("unexpected concurrent add error: %v", err)
		}
	}
	if succeeded != 1 || cycles != 1 {
		t.Fatalf("successes=%d cycles=%d", succeeded, cycles)
	}
}

func TestSelectorIsScopedToProject(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "istok.db")
	projects := newProjects(t, database)
	firstProject := initProject(t, projects, "first")
	secondProject := initProject(t, projects, "second")
	repo := newRepository(t, database)
	first, err := repo.Create(ctx, task.CreateInput{ProjectID: firstProject.ID, Title: "first"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(ctx, task.CreateInput{ProjectID: secondProject.ID, Title: "second"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if first.Number != 1 || second.Number != 1 {
		t.Fatalf("numbers = %d, %d", first.Number, second.Number)
	}
	value, err := repo.Resolve(ctx, task.Selector{ProjectID: secondProject.ID, Number: 1}, false)
	if err != nil || value.ID != second.ID {
		t.Fatalf("Resolve number = %#v, %v", value, err)
	}
	if _, err = repo.Resolve(ctx, task.Selector{ProjectID: secondProject.ID, ID: first.ID}, false); task.ErrorCode(err) != task.CodeNotFound {
		t.Fatalf("wrong project UUID = %v", err)
	}
}

func TestConcurrentLifecycleCASHasOneWinner(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "istok.db")
	projects := newProjects(t, database)
	p := initProject(t, projects, "cas")
	left, right := newRepository(t, database), newRepository(t, database)
	value, err := left.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "task"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	first, second := "first", "second"
	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for index, patch := range []task.Patch{{Title: &first}, {Title: &second}} {
		repository := []*Repository{left, right}[index]
		group.Add(1)
		go func(repository *Repository, patch task.Patch) {
			defer group.Done()
			<-start
			_, err := repository.Update(ctx, value.ID, value.Revision, patch, actor)
			errs <- err
		}(repository, patch)
	}
	close(start)
	group.Wait()
	close(errs)
	var winners, conflicts int
	for err := range errs {
		if err == nil {
			winners++
		} else if task.ErrorCode(err) == task.CodeRevisionConflict {
			conflicts++
		} else {
			t.Fatalf("update error=%v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
	result, err := right.Get(ctx, value.ID, false)
	if err != nil || result.Revision != 2 {
		t.Fatalf("final=%#v, %v", result, err)
	}
	events, err := right.Events(ctx, value.ID)
	if err != nil || len(events) != 2 || events[1].Type != "updated" {
		t.Fatalf("events=%#v, %v", events, err)
	}
}

func TestListDoesNotDeadlockOnSingleConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, _, p := newTaskRepository(t)
	repo.db.SetMaxOpenConns(1)
	first, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "first"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "second"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AddDependency(ctx, first.ID, second.ID, second.Revision, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.List(ctx, p.ID, task.ListOptions{}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestShowIncludesDoneAndDeletedDependencySummariesAndRawDependencies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	repo, _, p := newTaskRepository(t)
	repo.db.SetMaxOpenConns(1)

	target, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "target"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	openBlocker, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "open blocker"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	doneBlocker, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "done blocker"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	deletedBlocker, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "deleted blocker"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	openDependent, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "open dependent"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	doneDependent, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "done dependent"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	deletedDependent, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "deleted dependent"}, actor)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = repo.db.ExecContext(ctx, `UPDATE tasks SET status='done' WHERE id=?`, doneBlocker.ID); err != nil {
		t.Fatal(err)
	}

	target, err = repo.AddDependency(ctx, openBlocker.ID, target.ID, target.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	target, err = repo.AddDependency(ctx, doneBlocker.ID, target.ID, target.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	target, err = repo.AddDependency(ctx, deletedBlocker.ID, target.ID, target.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = repo.db.ExecContext(ctx, `UPDATE tasks SET deleted_at=? WHERE id=?`, deletedAt, deletedBlocker.ID); err != nil {
		t.Fatal(err)
	}

	_, err = repo.AddDependency(ctx, target.ID, openDependent.ID, openDependent.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.AddDependency(ctx, target.ID, doneDependent.ID, doneDependent.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.AddDependency(ctx, target.ID, deletedDependent.ID, deletedDependent.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}

	// Update dependent terminal statuses so summary state rendering and status filtering are meaningful.
	if _, err = repo.db.ExecContext(ctx, `UPDATE tasks SET status='done' WHERE id=?`, doneDependent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.db.ExecContext(ctx, `UPDATE tasks SET deleted_at=? WHERE id=?`, deletedAt, deletedDependent.ID); err != nil {
		t.Fatal(err)
	}

	target, err = repo.Comment(ctx, target.ID, target.Revision, "observer comment", actor)
	if err != nil {
		t.Fatal(err)
	}

	show, err := repo.Show(ctx, task.Selector{ProjectID: p.ID, ID: target.ID}, false)
	if err != nil {
		t.Fatalf("Show()=%#v, %v", show, err)
	}

	if len(show.Incoming) != 3 {
		t.Fatalf("incoming dependency count = %d, want 3", len(show.Incoming))
	}
	if len(show.Outgoing) != 3 {
		t.Fatalf("outgoing dependency count = %d, want 3", len(show.Outgoing))
	}
	if len(show.Blockers) != 3 {
		t.Fatalf("blocker summary count = %d, want 3", len(show.Blockers))
	}
	if len(show.Dependents) != 3 {
		t.Fatalf("dependent summary count = %d, want 3", len(show.Dependents))
	}

	incoming := map[string]struct{}{
		openBlocker.ID:    {},
		doneBlocker.ID:    {},
		deletedBlocker.ID: {},
	}
	for _, dependency := range show.Incoming {
		delete(incoming, dependency.BlockerTaskID)
	}
	if len(incoming) != 0 {
		t.Fatalf("Show() outgoing raw dependencies misses blockers: %#v", incoming)
	}

	outgoing := map[string]struct{}{
		openDependent.ID:    {},
		doneDependent.ID:    {},
		deletedDependent.ID: {},
	}
	for _, dependency := range show.Outgoing {
		delete(outgoing, dependency.BlockedTaskID)
	}
	if len(outgoing) != 0 {
		t.Fatalf("Show() incoming raw dependencies misses dependents: %#v", outgoing)
	}

	blockerNumbers := make([]int64, len(show.Blockers))
	for index, blocker := range show.Blockers {
		blockerNumbers[index] = blocker.Number
	}
	if !sort.SliceIsSorted(blockerNumbers, func(i, j int) bool { return blockerNumbers[i] < blockerNumbers[j] }) {
		t.Fatalf("blocker summaries are unsorted: %#v", blockerNumbers)
	}

	dependentNumbers := make([]int64, len(show.Dependents))
	for index, dependent := range show.Dependents {
		dependentNumbers[index] = dependent.Number
	}
	if !sort.SliceIsSorted(dependentNumbers, func(i, j int) bool { return dependentNumbers[i] < dependentNumbers[j] }) {
		t.Fatalf("dependent summaries are unsorted: %#v", dependentNumbers)
	}

	if len(show.Events) < 5 {
		t.Fatalf("show event count = %d, want at least 5", len(show.Events))
	}
	if show.Events[0].Type != "created" {
		t.Fatalf("first event type = %s, want created", show.Events[0].Type)
	}
	if show.Events[len(show.Events)-1].Type != "commented" {
		t.Fatalf("last event type = %s, want commented", show.Events[len(show.Events)-1].Type)
	}

	hasDoneBlocker := false
	hasDeletedBlocker := false
	for _, blocker := range show.Blockers {
		if blocker.ID == doneBlocker.ID {
			if blocker.Status != task.StatusDone || blocker.Title != doneBlocker.Title {
				t.Fatalf("done blocker summary = %#v", blocker)
			}
			hasDoneBlocker = true
		}
		if blocker.ID == deletedBlocker.ID {
			if blocker.DeletedAt == nil || blocker.Title != deletedBlocker.Title {
				t.Fatalf("deleted blocker summary = %#v", blocker)
			}
			hasDeletedBlocker = true
		}
	}
	if !hasDoneBlocker || !hasDeletedBlocker {
		t.Fatalf("show blockers missing expected statuses: done=%t deleted=%t", hasDoneBlocker, hasDeletedBlocker)
	}

	hasDeletedDependent := false
	hasDoneDependent := false
	for _, dependent := range show.Dependents {
		if dependent.ID == doneDependent.ID {
			if dependent.Status != task.StatusDone || dependent.Title != doneDependent.Title {
				t.Fatalf("done dependent summary = %#v", dependent)
			}
			hasDoneDependent = true
		}
		if dependent.ID == deletedDependent.ID {
			if dependent.DeletedAt == nil || dependent.Title != deletedDependent.Title {
				t.Fatalf("deleted dependent summary = %#v", dependent)
			}
			hasDeletedDependent = true
		}
	}
	if !hasDoneDependent || !hasDeletedDependent {
		t.Fatalf("show dependents missing expected statuses: done=%t deleted=%t", hasDoneDependent, hasDeletedDependent)
	}
}

func TestShowDoesNotDeadlockOnSingleConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, _, p := newTaskRepository(t)
	repo.db.SetMaxOpenConns(1)

	target, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "target"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "blocker"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	target, err = repo.AddDependency(ctx, blocker.ID, target.ID, target.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Show(ctx, task.Selector{ProjectID: p.ID, ID: target.ID}, false); err != nil {
		t.Fatalf("Show() error = %v", err)
	}
}

func TestBlockTransitionsAndDeletedMutation(t *testing.T) {
	ctx := context.Background()
	repo, _, p := newTaskRepository(t)
	v, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "task"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	v, err = repo.Block(ctx, v.ID, v.Revision, "waiting", actor)
	if err != nil || v.Status != task.StatusBlocked {
		t.Fatalf("Block()=%#v,%v", v, err)
	}
	if _, err = repo.Block(ctx, v.ID, v.Revision, "again", actor); task.ErrorCode(err) != task.CodeInvalidTransition {
		t.Fatalf("Block transition=%v", err)
	}
	v, err = repo.Unblock(ctx, v.ID, v.Revision, "continue", actor)
	if err != nil || v.Status != task.StatusOpen {
		t.Fatalf("Unblock()=%#v,%v", v, err)
	}
	v, err = repo.Delete(ctx, v.ID, v.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Comment(ctx, v.ID, v.Revision, "no", actor); task.ErrorCode(err) != task.CodeConflict {
		t.Fatalf("deleted comment=%v", err)
	}
}

func TestFullLifecycleEventsShowAndStatusList(t *testing.T) {
	ctx := context.Background()
	repo, _, p := newTaskRepository(t)
	value, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "task"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	title := "updated"
	value, err = repo.Update(ctx, value.ID, value.Revision, task.Patch{Title: &title}, actor)
	if err != nil {
		t.Fatal(err)
	}
	value, err = repo.Comment(ctx, value.ID, value.Revision, "comment", actor)
	if err != nil {
		t.Fatal(err)
	}
	value, err = repo.Progress(ctx, value.ID, value.Revision, "progress", actor)
	if err != nil {
		t.Fatal(err)
	}
	value, err = repo.Block(ctx, value.ID, value.Revision, "reason", actor)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := repo.List(ctx, p.ID, task.ListOptions{Statuses: []task.Status{task.StatusBlocked}})
	if err != nil || len(blocked) != 1 || blocked[0].ID != value.ID {
		t.Fatalf("blocked list=%#v, %v", blocked, err)
	}
	value, err = repo.Unblock(ctx, value.ID, value.Revision, "note", actor)
	if err != nil || value.Revision != 6 || value.Status != task.StatusOpen {
		t.Fatalf("Unblock()=%#v, %v", value, err)
	}
	blocker, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "blocker"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AddDependency(ctx, blocker.ID, value.ID, value.Revision, actor); err != nil {
		t.Fatal(err)
	}
	show, err := repo.Show(ctx, task.Selector{ProjectID: p.ID, ID: value.ID}, false)
	if err != nil || len(show.Events) != 7 || len(show.Incoming) != 1 || len(show.Outgoing) != 0 {
		t.Fatalf("Show()=%#v, %v", show, err)
	}
	wantTypes := []string{"created", "updated", "commented", "progress", "blocked", "unblocked"}
	wantBodies := []string{"", "", "comment", "progress", "reason", "note"}
	for i := range wantTypes {
		event := show.Events[i]
		if event.Type != wantTypes[i] || event.Body != wantBodies[i] || event.TaskRevision != int64(i+1) || event.Actor != actor {
			t.Fatalf("event %d=%#v", i, event)
		}
	}
}

func TestDependencyInvariantsAndDatabaseConstraints(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "istok.db")
	projects := newProjects(t, database)
	firstProject := initProject(t, projects, "first")
	secondProject := initProject(t, projects, "second")
	repo := newRepository(t, database)
	first, err := repo.Create(ctx, task.CreateInput{ProjectID: firstProject.ID, Title: "first"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(ctx, task.CreateInput{ProjectID: firstProject.ID, Title: "second"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.Create(ctx, task.CreateInput{ProjectID: secondProject.ID, Title: "other"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AddDependency(ctx, first.ID, first.ID, first.Revision, actor); task.ErrorCode(err) != task.CodeInvalid {
		t.Fatalf("self=%v", err)
	}
	if _, err = repo.AddDependency(ctx, first.ID, other.ID, other.Revision, actor); task.ErrorCode(err) != task.CodeConflict {
		t.Fatalf("cross project=%v", err)
	}
	blocked, err := repo.AddDependency(ctx, first.ID, second.ID, second.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	events, err := repo.Events(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.AddDependency(ctx, first.ID, second.ID, blocked.Revision, actor); task.ErrorCode(err) != task.CodeConflict {
		t.Fatalf("duplicate=%v", err)
	}
	after, err := repo.Get(ctx, second.ID, false)
	if err != nil || after.Revision != blocked.Revision {
		t.Fatalf("after duplicate=%#v, %v", after, err)
	}
	afterEvents, err := repo.Events(ctx, second.ID)
	if err != nil || len(afterEvents) != len(events) {
		t.Fatalf("after events=%#v, %v", afterEvents, err)
	}
	for _, query := range []string{
		`INSERT INTO task_dependencies(id,project_id,blocker_task_id,blocked_task_id,edge_type,created_at) VALUES('00000000-0000-7000-8000-000000000001', '` + firstProject.ID + `', '` + first.ID + `', '` + other.ID + `', 'blocks', 'now')`,
		`INSERT INTO task_dependencies(id,project_id,blocker_task_id,blocked_task_id,edge_type,created_at) VALUES('00000000-0000-7000-8000-000000000002', '` + firstProject.ID + `', '` + first.ID + `', '` + second.ID + `', 'wrong', 'now')`,
		`INSERT INTO task_dependencies(id,project_id,blocker_task_id,blocked_task_id,edge_type,created_at) VALUES('00000000-0000-7000-8000-000000000003', '` + firstProject.ID + `', '` + first.ID + `', '` + first.ID + `', 'blocks', 'now')`,
	} {
		if _, err := repo.db.ExecContext(ctx, query); err == nil {
			t.Fatalf("invalid dependency insert succeeded: %s", query)
		}
	}
}

func TestReadyAfterDoneOrDeletedBlocker(t *testing.T) {
	ctx := context.Background()
	for _, fixture := range []string{"done", "deleted"} {
		t.Run(fixture, func(t *testing.T) {
			repo, _, p := newTaskRepository(t)
			a, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "A"}, actor)
			if err != nil {
				t.Fatal(err)
			}
			b, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "B"}, actor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.AddDependency(ctx, a.ID, b.ID, b.Revision, actor); err != nil {
				t.Fatal(err)
			}
			ready, err := repo.Ready(ctx, p.ID)
			if err != nil || len(ready) != 1 || ready[0].ID != a.ID {
				t.Fatalf("before fixture=%#v, %v", ready, err)
			}
			if fixture == "done" {
				_, err = repo.db.ExecContext(ctx, `UPDATE tasks SET status='done' WHERE id=?`, a.ID)
			} else {
				_, err = repo.db.ExecContext(ctx, `UPDATE tasks SET deleted_at='fixture' WHERE id=?`, a.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			ready, err = repo.Ready(ctx, p.ID)
			if err != nil || len(ready) != 1 || ready[0].ID != b.ID {
				t.Fatalf("after fixture=%#v, %v", ready, err)
			}
		})
	}
}

func TestDeleteActiveBlockerGuard(t *testing.T) {
	ctx := context.Background()
	repo, _, p := newTaskRepository(t)
	a, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "A"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "B"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	b, err = repo.AddDependency(ctx, a.ID, b.ID, b.Revision, actor)
	if err != nil {
		t.Fatal(err)
	}
	events, err := repo.Events(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Delete(ctx, a.ID, a.Revision, actor); task.ErrorCode(err) != task.CodeConflict {
		t.Fatalf("Delete guard=%v", err)
	}
	after, err := repo.Get(ctx, a.ID, false)
	if err != nil || after.Revision != a.Revision {
		t.Fatalf("after guard=%#v, %v", after, err)
	}
	afterEvents, err := repo.Events(ctx, a.ID)
	if err != nil || len(afterEvents) != len(events) {
		t.Fatalf("after guard events=%#v, %v", afterEvents, err)
	}
	if _, err = repo.RemoveDependency(ctx, a.ID, b.ID, b.Revision, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Delete(ctx, a.ID, a.Revision, actor); err != nil {
		t.Fatalf("Delete after remove=%v", err)
	}
}

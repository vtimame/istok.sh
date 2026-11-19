package handoffapp

import (
	"context"
	"testing"
	"time"

	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type fakeTasks struct {
	events map[string][]task.Event
}

func (f fakeTasks) Show(_ context.Context, selector task.Selector, _ bool) (task.Show, error) {
	return task.Show{Task: task.Task{ID: selector.ID}, Events: f.events[selector.ID]}, nil
}

type fakeRuns struct {
	runs []run.Run
}

func (f fakeRuns) ListRuns(_ context.Context, options run.ListOptions) ([]run.Run, error) {
	result := []run.Run{}
	for _, value := range f.runs {
		if options.TaskID != "" && value.TaskID != options.TaskID {
			continue
		}
		if len(options.Statuses) > 0 && value.Status != options.Statuses[0] {
			continue
		}
		result = append(result, value)
	}

	return result, nil
}

func TestBriefStartsAfterTheLatestHandoff(t *testing.T) {
	events := []task.Event{
		{Type: task.EventProgress, Body: "old"},
		{Type: task.EventHandoff, Body: "first handoff"},
		{Type: task.EventProgress, Body: "between"},
		{Type: task.EventHandoff, Body: "latest handoff"},
		{Type: "updated", Body: "not a note"},
	}
	for i := range 7 {
		events = append(events, task.Event{Type: task.EventProgress, Body: string(rune('a' + i))})
	}

	handoff, recent := notes(events)
	if handoff == nil || handoff.Body != "latest handoff" {
		t.Fatalf("handoff = %+v", handoff)
	}
	if len(recent) != noteLimit || recent[0].Body != "c" || recent[len(recent)-1].Body != "g" {
		t.Fatalf("notes = %+v", recent)
	}

	none, all := notes([]task.Event{{Type: task.EventBlocked, Body: "waiting"}, {Type: task.EventComment, Body: "why"}})
	if none != nil || len(all) != 2 {
		t.Fatalf("without a handoff: handoff %+v notes %+v", none, all)
	}
}

func TestResumeKeepsLiveAndRecentRunsOfTheActor(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	owner := run.ActorSnapshot{ID: "agent-a", Kind: "agent", Name: "A"}
	other := run.ActorSnapshot{ID: "agent-b", Kind: "agent", Name: "B"}

	service := NewService(fakeTasks{}, fakeRuns{runs: []run.Run{
		{ID: "live", TaskID: "task-live", Status: run.StatusActive, LeaseOwner: owner, ExpiresAt: now.Add(10 * time.Minute)},
		{ID: "recent", TaskID: "task-recent", Status: run.StatusActive, LeaseOwner: owner, ExpiresAt: now.Add(-time.Hour)},
		{ID: "stale", TaskID: "task-stale", Status: run.StatusActive, LeaseOwner: owner, ExpiresAt: now.Add(-48 * time.Hour)},
		{ID: "foreign", TaskID: "task-foreign", Status: run.StatusActive, LeaseOwner: other, ExpiresAt: now.Add(time.Minute)},
	}})
	service.now = func() time.Time { return now }

	mine, err := service.Resume(context.Background(), ResumeInput{ProjectID: "project", ActorID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(mine); len(got) != 2 || got[0] != "task-live" || got[1] != "task-recent" {
		t.Fatalf("resume for the actor = %v", got)
	}
	if mine[0].ActiveRun.LeaseExpired || !mine[1].ActiveRun.LeaseExpired {
		t.Fatalf("lease state: live %+v recent %+v", mine[0].ActiveRun, mine[1].ActiveRun)
	}

	everyone, err := service.Resume(context.Background(), ResumeInput{ProjectID: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(everyone); len(got) != 3 || got[2] != "task-foreign" {
		t.Fatalf("resume for every agent = %v", got)
	}
}

func taskIDs(briefs []Brief) []string {
	result := make([]string, 0, len(briefs))
	for _, brief := range briefs {
		result = append(result, brief.Task.ID)
	}

	return result
}

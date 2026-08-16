package task

import (
	"testing"
	"time"
)

func TestUUIDv7ValidationRequiresCanonicalUUIDv7(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if !IsUUIDv7(id) {
		t.Fatalf("IsUUIDv7(%q) = false", id)
	}
	if IsUUIDv7("{"+id+"}") || IsUUIDv7("00000000-0000-4000-8000-000000000000") {
		t.Fatal("noncanonical or non-v7 UUID accepted")
	}
}

func TestPatchAndTransitionsAreStorageIndependent(t *testing.T) {
	title := "  revised  "
	value := Task{Status: StatusOpen, Title: "initial"}
	if err := (Patch{Title: &title}).Apply(&value); err != nil {
		t.Fatal(err)
	}
	if value.Title != "revised" {
		t.Fatalf("title = %q", value.Title)
	}
	if err := value.Block(); err != nil || value.Status != StatusBlocked {
		t.Fatalf("Block() = %v, status %s", err, value.Status)
	}
	if err := value.Block(); ErrorCode(err) != CodeInvalidTransition {
		t.Fatalf("second Block() error = %v", err)
	}
	if err := value.Unblock(); err != nil || value.Status != StatusOpen {
		t.Fatalf("Unblock() = %v, status %s", err, value.Status)
	}
}

func TestDerivedStatePrecedenceAndActiveBlockers(t *testing.T) {
	deletedAt := time.Now()

	for _, test := range []struct {
		name          string
		target        Task
		activeRun     bool
		blockers      []TaskSummary
		expectedState EffectiveState
	}{
		{
			name:          "done wins over inconsistent active run",
			target:        Task{Status: StatusDone},
			activeRun:     true,
			expectedState: EffectiveStateDone,
		},
		{
			name:          "active run",
			target:        Task{Status: StatusOpen},
			activeRun:     true,
			blockers:      []TaskSummary{{Status: StatusOpen}},
			expectedState: EffectiveStateInProgress,
		},
		{
			name:          "persisted blocked",
			target:        Task{Status: StatusBlocked},
			expectedState: EffectiveStateBlocked,
		},
		{
			name:          "active blocker",
			target:        Task{Status: StatusOpen},
			blockers:      []TaskSummary{{Status: StatusOpen}},
			expectedState: EffectiveStateBlocked,
		},
		{
			name:          "done and deleted blockers are inactive",
			target:        Task{Status: StatusOpen},
			blockers:      []TaskSummary{{Status: StatusDone}, {Status: StatusOpen, DeletedAt: &deletedAt}},
			expectedState: EffectiveStateReady,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := DeriveState(test.target, test.activeRun, test.blockers); got != test.expectedState {
				t.Fatalf("DeriveState() = %q, want %q", got, test.expectedState)
			}
		})
	}
}

package runrepo

import (
	"context"
	"strings"
	"testing"
	"time"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	runmodel "github.com/vtimame/istok.sh/internal/run"
)

func TestRunRepoOwnerResumesExpiredLeaseInPlace(t *testing.T) {
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	f := newRunFixtureWithClock(t, func() time.Time { return now })
	projectValue := newProject(t, f, "lease-resume")
	targetTask := newTask(t, f, projectValue.ID, "target")
	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", contextmodel.KindDecision)
	claimed := claimRun(t, f, targetTask.ID, newSnapshot(t, projectValue.ID, nil, contextValue))

	now = now.Add(40 * time.Minute)

	other := runmodel.ActorSnapshot{ID: "other-agent", Kind: "agent", Name: "Other"}
	if _, err := f.runs.Heartbeat(context.Background(), runmodel.HeartbeatInput{RunID: claimed.ID, LeaseID: claimed.LeaseID, LeaseDuration: time.Minute, EventID: mustRunID(t)}, other); runmodel.ErrorCode(err) != runmodel.CodeConflict {
		t.Fatalf("Heartbeat() by another actor error = %v, want conflict", err)
	}

	resumed, err := f.runs.Heartbeat(context.Background(), runmodel.HeartbeatInput{RunID: claimed.ID, LeaseID: claimed.LeaseID, LeaseDuration: 15 * time.Minute, EventID: mustRunID(t)}, testRunActor)
	if err != nil {
		t.Fatalf("owner Heartbeat() on an expired lease error = %v", err)
	}
	if resumed.LeaseID != claimed.LeaseID || !resumed.ExpiresAt.After(now) || resumed.Status != runmodel.StatusActive {
		t.Fatalf("resumed run = %#v", resumed)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id=? AND type='run_recovered' AND body=?", targetTask.ID, resumeLeaseReason); got != 1 {
		t.Fatalf("resume events = %d, want 1", got)
	}

	if _, err := f.runs.Heartbeat(context.Background(), runmodel.HeartbeatInput{RunID: claimed.ID, LeaseID: claimed.LeaseID, LeaseDuration: 15 * time.Minute, EventID: mustRunID(t)}, testRunActor); err != nil {
		t.Fatalf("Heartbeat() on a live lease error = %v", err)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id=? AND type='run_recovered'", targetTask.ID); got != 1 {
		t.Fatalf("a live heartbeat recorded a resume event: %d", got)
	}
}

func TestRunRepoExpiredLeaseWithRunningExecutionStillNeedsRecovery(t *testing.T) {
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	f := newRunFixtureWithClock(t, func() time.Time { return now })
	projectValue := newProject(t, f, "lease-running")
	targetTask := newTask(t, f, projectValue.ID, "target")
	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", contextmodel.KindDecision)
	claimed := claimRun(t, f, targetTask.ID, newSnapshot(t, projectValue.ID, nil, contextValue))
	startExecution(t, f, claimed.ID)

	now = now.Add(40 * time.Minute)

	if _, err := f.runs.Heartbeat(context.Background(), runmodel.HeartbeatInput{RunID: claimed.ID, LeaseID: claimed.LeaseID, LeaseDuration: time.Minute, EventID: mustRunID(t)}, testRunActor); runmodel.ErrorCode(err) != runmodel.CodeConflict {
		t.Fatalf("Heartbeat() with a running execution error = %v, want conflict", err)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM task_events WHERE task_id=? AND type='run_recovered'", targetTask.ID); got != 0 {
		t.Fatalf("resume events = %d, want 0", got)
	}
}

func TestRunRepoRecordEvidenceCreatesExecutionAndAttestedValidation(t *testing.T) {
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	f := newRunFixtureWithClock(t, func() time.Time { return now })
	projectValue := newProject(t, f, "evidence")
	targetTask := newTask(t, f, projectValue.ID, "target")
	contextValue := newContextRecord(t, f, projectValue.ID, "snapshot", "snapshot", contextmodel.KindDecision)
	claimed := claimRun(t, f, targetTask.ID, newSnapshot(t, projectValue.ID, nil, contextValue))

	tail := strings.Repeat("x", runmodel.MaxEvidenceOutputTail) + "END"
	evidence, err := f.runs.RecordEvidence(context.Background(), runmodel.RecordEvidenceInput{
		ExecutionID:  mustRunID(t),
		ValidationID: mustRunID(t),
		RunID:        claimed.ID,
		LeaseID:      claimed.LeaseID,
		CWD:          "/project",
		Status:       runmodel.ValidationStatusPassed,
		Summary:      "phone e2e login works",
		OutputTail:   tail,
	}, testRunActor)
	if err != nil {
		t.Fatalf("RecordEvidence() error = %v", err)
	}

	if evidence.Execution.Status != runmodel.ExecutionSucceeded || len(evidence.Execution.Argv) != 1 || evidence.Execution.Argv[0] != runmodel.ManualCommand {
		t.Fatalf("evidence execution = %#v", evidence.Execution)
	}
	if evidence.Validation.Source != runmodel.ValidationSourceAttested || evidence.Validation.Status != runmodel.ValidationStatusPassed || evidence.Validation.Command != runmodel.ManualCommand {
		t.Fatalf("evidence validation = %#v", evidence.Validation)
	}
	if !strings.HasPrefix(evidence.Validation.Summary, "phone e2e login works\n\nOutput tail:\n") || !strings.HasSuffix(evidence.Validation.Summary, "END") || strings.Count(evidence.Validation.Summary, "x") >= runmodel.MaxEvidenceOutputTail {
		t.Fatalf("evidence summary keeps %d bytes", len(evidence.Validation.Summary))
	}

	exitCode := 1
	failed, err := f.runs.RecordEvidence(context.Background(), runmodel.RecordEvidenceInput{
		ExecutionID:  mustRunID(t),
		ValidationID: mustRunID(t),
		RunID:        claimed.ID,
		LeaseID:      claimed.LeaseID,
		CWD:          "/project",
		Status:       runmodel.ValidationStatusFailed,
		Summary:      "pipeline red",
		Command:      "gitlab pipeline 812",
		ExitCode:     &exitCode,
	}, testRunActor)
	if err != nil {
		t.Fatalf("failed RecordEvidence() error = %v", err)
	}
	if failed.Execution.Status != runmodel.ExecutionFailed || failed.Validation.Command != "gitlab pipeline 812" || failed.Validation.ExitCode == nil || *failed.Validation.ExitCode != 1 {
		t.Fatalf("failed evidence = %#v", failed)
	}

	if _, err := f.runs.RecordEvidence(context.Background(), runmodel.RecordEvidenceInput{
		ExecutionID:  mustRunID(t),
		ValidationID: mustRunID(t),
		RunID:        claimed.ID,
		LeaseID:      mustRunID(t),
		CWD:          "/project",
		Status:       runmodel.ValidationStatusPassed,
		Summary:      "stale lease",
	}, testRunActor); runmodel.ErrorCode(err) != runmodel.CodeConflict {
		t.Fatalf("RecordEvidence() with a foreign lease error = %v, want conflict", err)
	}
	if got := countRows(t, f.db, "SELECT COUNT(*) FROM executions WHERE run_id=?", claimed.ID); got != 2 {
		t.Fatalf("executions = %d, want 2 (the rejected call must not leave rows)", got)
	}
}

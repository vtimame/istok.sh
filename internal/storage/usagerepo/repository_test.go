package usagerepo

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/storage"
	"github.com/vtimame/istok.sh/internal/usage"
)

func TestRepositoryRecordsCountsAndPrunesCalls(t *testing.T) {
	ctx := context.Background()
	repository, db := newRepository(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	calls := []usage.Call{
		{ID: "01", SessionID: "s1", ClientName: "claude-code", ClientVersion: "2.0", ActorID: "agent", ProjectID: "p1", Tool: "task_progress", StartedAt: now.Add(-time.Hour), Duration: 12 * time.Millisecond, Outcome: usage.OK},
		{ID: "02", SessionID: "s1", ClientName: "claude-code", ActorID: "agent", ProjectID: "p1", Tool: "task_progress", StartedAt: now.Add(-50 * time.Minute), Outcome: usage.OK},
		{ID: "03", SessionID: "s1", ClientName: "claude-code", ActorID: "agent", ProjectID: "p1", Tool: "task_progress", StartedAt: now.Add(-40 * time.Minute), Outcome: usage.Error, ErrorCode: "revision_conflict"},
		{ID: "04", SessionID: "s2", ActorID: "agent", Tool: "project_current", StartedAt: now.Add(-30 * time.Minute), Outcome: usage.Error, ErrorCode: "not_found"},
		{ID: "05", SessionID: "s3", ActorID: "agent", ProjectID: "p2", Tool: "health", StartedAt: now.Add(-100 * 24 * time.Hour), Outcome: usage.OK},
	}
	for _, call := range calls {
		if err := repository.Record(ctx, call); err != nil {
			t.Fatalf("Record(%s) error = %v", call.ID, err)
		}
	}

	var projectID sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT project_id FROM mcp_calls WHERE id='04'`).Scan(&projectID); err != nil {
		t.Fatalf("read project_id: %v", err)
	}
	if projectID.Valid {
		t.Fatalf("project_id = %q, want NULL for a call outside a project", projectID.String)
	}

	recent, err := repository.Counts(ctx, usage.Filter{Since: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}
	if got := countFor(recent, "s1", "task_progress", usage.OK); got != 2 {
		t.Errorf("s1 task_progress ok = %d, want 2", got)
	}
	if got := countFor(recent, "s1", "task_progress", usage.Error); got != 1 {
		t.Errorf("s1 task_progress error = %d, want 1", got)
	}
	if got := countFor(recent, "s2", "project_current", usage.Error); got != 1 {
		t.Errorf("s2 project_current error = %d, want 1", got)
	}
	if got := countFor(recent, "s3", "health", usage.OK); got != 0 {
		t.Errorf("old call is inside the period: %d", got)
	}

	scoped, err := repository.Counts(ctx, usage.Filter{Since: now.Add(-24 * time.Hour), ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Counts(project) error = %v", err)
	}
	for _, value := range scoped {
		if value.SessionID != "s1" {
			t.Errorf("project filter returned session %q", value.SessionID)
		}
	}

	if err := repository.Prune(ctx, now.Add(-usage.Retention)); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_calls`).Scan(&remaining); err != nil {
		t.Fatalf("count calls: %v", err)
	}
	if remaining != 4 {
		t.Fatalf("calls after prune = %d, want 4", remaining)
	}
}

func TestRepositoryRejectsAnErrorWithoutCode(t *testing.T) {
	repository, _ := newRepository(t)

	err := repository.Record(context.Background(), usage.Call{ID: "01", SessionID: "s", ActorID: "a", Tool: "health", StartedAt: time.Now(), Outcome: usage.Error})
	if err == nil {
		t.Fatal("Record() accepted an error outcome without an error code")
	}
}

func TestStampsCompareInTimeOrder(t *testing.T) {
	base := time.Date(2026, 11, 18, 12, 0, 5, 0, time.UTC)

	if !(stamp(base) < stamp(base.Add(100*time.Millisecond))) {
		t.Fatalf("stamp(%v) does not sort before a later stamp in the same second", base)
	}
}

func countFor(counts []usage.ToolCount, session, tool string, outcome usage.Outcome) int {
	total := 0
	for _, value := range counts {
		if value.SessionID == session && value.Tool == tool && value.Outcome == outcome {
			total += value.Calls
		}
	}

	return total
}

func newRepository(t *testing.T) (*Repository, *sql.DB) {
	t.Helper()

	var db *sql.DB
	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: filepath.Join(t.TempDir(), "istok.db")}),
		fx.Populate(&db),
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start test database: %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	return New(db), db
}

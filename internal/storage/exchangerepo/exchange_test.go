package exchangerepo

import (
	"context"
	"strings"
	"testing"

	"github.com/vtimame/istok.sh/internal/exchange"
)

func TestExportLeavesLocalDataBehind(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")

	bundle := source.export(projectID)

	if bundle.Format != exchange.Format {
		t.Errorf("format = %q", bundle.Format)
	}
	for _, name := range []string{"project_roots", "artifacts", "context_snapshot_retrieval_items", "project_task_counters"} {
		if _, ok := bundle.Tables[name]; ok {
			t.Errorf("bundle contains local table %s", name)
		}
	}

	if _, ok := bundle.Tables["projects"][0]["deleted_at"]; ok {
		t.Error("project rows carry deleted_at")
	}

	for _, name := range []string{"context_records", "knowledge_items"} {
		for _, row := range bundle.Tables[name] {
			if row["visibility"] != "shared" {
				t.Errorf("%s exported a %v row", name, row["visibility"])
			}
		}
	}
	if got := len(bundle.Tables["context_snapshot_items"]); got != 1 {
		t.Errorf("snapshot items = %d, want only the shared one", got)
	}
	if got := len(bundle.Tables["context_events"]); got != 1 {
		t.Errorf("context events = %d, want only the shared record's", got)
	}

	if len(bundle.Projects) != 1 || bundle.Projects[0].Tasks != 2 {
		t.Errorf("projects summary = %+v", bundle.Projects)
	}
}

func TestExportRejectsUnknownProject(t *testing.T) {
	source := newDevice(t)

	_, err := source.repo.Export(context.Background(), []string{projectID})
	if code, _ := exchange.ErrorCode(err); code != exchange.CodeNotFound {
		t.Fatalf("Export() error = %v, want not_found", err)
	}
}

func TestImportIntoEmptyDeviceReproducesTheProject(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)

	report := target.importBundle(source.export(projectID), false)

	for _, name := range []string{"tasks", "task_events", "task_dependencies", "runs", "run_leases", "executions", "validations", "task_completions"} {
		if got, want := target.count("SELECT COUNT(*) FROM "+name), source.count("SELECT COUNT(*) FROM "+name); got != want {
			t.Errorf("%s rows = %d, want %d", name, got, want)
		}
	}
	if got := target.count("SELECT COUNT(*) FROM context_records"); got != 1 {
		t.Errorf("context records = %d, want 1 shared", got)
	}
	if got := target.count("SELECT COUNT(*) FROM knowledge_items"); got != 1 {
		t.Errorf("knowledge items = %d, want 1 shared", got)
	}
	if got := target.count("SELECT COUNT(*) FROM artifacts"); got != 0 {
		t.Errorf("artifacts = %d, want none", got)
	}
	if got := target.count("SELECT COUNT(*) FROM project_roots"); got != 0 {
		t.Errorf("project roots = %d, want an unbound project", got)
	}
	if got := target.count("SELECT next_number FROM project_task_counters WHERE project_id = ?", projectID); got != 3 {
		t.Errorf("next task number = %d, want 3", got)
	}

	if len(report.Projects) != 1 || !report.Projects[0].Created || report.Projects[0].Bound {
		t.Errorf("project outcome = %+v, want created and unbound", report.Projects)
	}
	if !containsText(report.Warnings, "no folder on this device") {
		t.Errorf("warnings = %q, want a binding hint", report.Warnings)
	}
	if len(report.Conflicts) != 0 {
		t.Errorf("conflicts = %+v", report.Conflicts)
	}
}

func TestImportTwiceChangesNothing(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	bundle := source.export(projectID)

	target.importBundle(bundle, false)
	report := target.importBundle(bundle, false)

	for name, counts := range report.Tables {
		if counts.Created != 0 || counts.Updated != 0 || counts.KeptLocal != 0 || counts.Skipped != 0 {
			t.Errorf("%s counts on second import = %+v", name, counts)
		}
	}
	if len(report.Renumbered) != 0 || len(report.Conflicts) != 0 {
		t.Errorf("second import renumbered %+v, conflicts %+v", report.Renumbered, report.Conflicts)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)

	report := target.importBundle(source.export(projectID), true)

	if !report.DryRun || report.Tables["tasks"].Created != 2 {
		t.Errorf("dry run report = %+v", report.Tables["tasks"])
	}
	if got := target.count("SELECT COUNT(*) FROM projects"); got != 0 {
		t.Errorf("projects after dry run = %d, want 0", got)
	}
}

func TestHigherRevisionWinsAndLowerIsKept(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.importBundle(source.export(projectID), false)

	source.exec("UPDATE tasks SET title = 'Add cursor pagination', revision = 2 WHERE id = ?", taskOneID)
	target.exec("UPDATE tasks SET title = 'Local edit', revision = 3 WHERE id = ?", taskTwoID)
	report := target.importBundle(source.export(projectID), false)

	if got := target.text("SELECT title FROM tasks WHERE id = ?", taskOneID); got != "Add cursor pagination" {
		t.Errorf("task one title = %q, want the newer incoming title", got)
	}
	if got := target.text("SELECT title FROM tasks WHERE id = ?", taskTwoID); got != "Local edit" {
		t.Errorf("task two title = %q, want the newer local title", got)
	}
	if counts := report.Tables["tasks"]; counts.Updated != 1 || counts.KeptLocal != 1 {
		t.Errorf("task counts = %+v", counts)
	}
	if len(report.Conflicts) != 0 {
		t.Errorf("conflicts = %+v, a lower incoming revision is not a conflict", report.Conflicts)
	}
}

func TestEqualRevisionWithDifferentContentKeepsLocal(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.importBundle(source.export(projectID), false)

	source.exec("UPDATE context_records SET body = 'Theirs', revision = 2 WHERE id = ?", sharedNoteID)
	target.exec("UPDATE context_records SET body = 'Ours', revision = 2 WHERE id = ?", sharedNoteID)
	report := target.importBundle(source.export(projectID), false)

	if got := target.text("SELECT body FROM context_records WHERE id = ?", sharedNoteID); got != "Ours" {
		t.Errorf("body = %q, want the local version", got)
	}
	if len(report.Conflicts) != 1 || report.Conflicts[0].Table != "context_records" {
		t.Errorf("conflicts = %+v, want one context record conflict", report.Conflicts)
	}
}

func TestLaterTaskIsRenumberedOnCollision(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.importBundle(source.export(projectID), false)

	// Both devices create task #3 while apart; the source's is newer.
	target.addTask("01a10000-0000-7000-8000-0000000000b1", 3, "Local third", stampAt(3*createdOffset))
	source.addTask("01a10000-0000-7000-8000-0000000000b2", 3, "Remote third", stampAt(4*createdOffset))
	report := target.importBundle(source.export(projectID), false)

	if got := target.count("SELECT number FROM tasks WHERE id = ?", "01a10000-0000-7000-8000-0000000000b1"); got != 3 {
		t.Errorf("local task number = %d, want it to keep #3", got)
	}
	if got := target.count("SELECT number FROM tasks WHERE id = ?", "01a10000-0000-7000-8000-0000000000b2"); got != 4 {
		t.Errorf("incoming task number = %d, want #4", got)
	}
	if len(report.Renumbered) != 1 || report.Renumbered[0].From != 3 || report.Renumbered[0].To != 4 {
		t.Errorf("renumbered = %+v", report.Renumbered)
	}
	if got := target.count("SELECT COUNT(*) FROM task_events WHERE task_id = ? AND type = 'renumbered'", "01a10000-0000-7000-8000-0000000000b2"); got != 1 {
		t.Errorf("renumbering events = %d, want 1", got)
	}
	if got := target.count("SELECT next_number FROM project_task_counters WHERE project_id = ?", projectID); got != 5 {
		t.Errorf("next task number = %d, want 5", got)
	}
}

func TestEarlierIncomingTaskKeepsItsNumber(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.importBundle(source.export(projectID), false)

	target.addTask("01a10000-0000-7000-8000-0000000000b1", 3, "Local third", stampAt(4*createdOffset))
	source.addTask("01a10000-0000-7000-8000-0000000000b2", 3, "Remote third", stampAt(3*createdOffset))
	report := target.importBundle(source.export(projectID), false)

	if got := target.count("SELECT number FROM tasks WHERE id = ?", "01a10000-0000-7000-8000-0000000000b2"); got != 3 {
		t.Errorf("incoming task number = %d, want #3", got)
	}
	if got := target.count("SELECT number FROM tasks WHERE id = ?", "01a10000-0000-7000-8000-0000000000b1"); got != 4 {
		t.Errorf("local task number = %d, want #4", got)
	}
	if len(report.Renumbered) != 1 || report.Renumbered[0].Title != "Local third" {
		t.Errorf("renumbered = %+v", report.Renumbered)
	}
}

func TestDependenciesFollowTheBlockedTask(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.importBundle(source.export(projectID), false)

	// Removing a dependency bumps the blocked task's revision.
	source.exec("DELETE FROM task_dependencies WHERE id = ?", dependencyID)
	source.exec("UPDATE tasks SET revision = revision + 1 WHERE id = ?", taskTwoID)
	target.importBundle(source.export(projectID), false)

	if got := target.count("SELECT COUNT(*) FROM task_dependencies"); got != 0 {
		t.Errorf("dependencies = %d, want the removal to arrive", got)
	}
}

func TestSecondActiveRunIsAConflict(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.importBundle(source.export(projectID), false)

	for _, device := range []struct {
		d     *device
		run   string
		snap  string
		lease string
	}{
		{target, "01a10000-0000-7000-8000-0000000000c1", "01a10000-0000-7000-8000-0000000000d1", "lease-local"},
		{source, "01a10000-0000-7000-8000-0000000000c2", "01a10000-0000-7000-8000-0000000000d2", "lease-remote"},
	} {
		device.d.exec("INSERT INTO context_snapshots (id, schema_version, project_id, generated_at, created_at) VALUES (?, '5', ?, ?, ?)",
			device.snap, projectID, stampAt(0), stampAt(0))
		device.d.exec("INSERT INTO runs (id, task_id, context_snapshot_id, status, "+actorColumns+", started_at, updated_at) VALUES (?, ?, ?, 'active', "+actorValues+", ?, ?)",
			device.run, taskTwoID, device.snap, stampAt(0), stampAt(0))
		device.d.exec("INSERT INTO run_leases (run_id, lease_id, owner_id, owner_kind, owner_name, heartbeat_at, expires_at, updated_at) VALUES (?, ?, 'agent-1', 'agent', 'Agent', ?, ?, ?)",
			device.run, device.lease, stampAt(0), stampAt(0), stampAt(0))
	}

	report := target.importBundle(source.export(projectID), false)

	if got := target.count("SELECT COUNT(*) FROM runs WHERE status = 'active'"); got != 1 {
		t.Errorf("active runs = %d, want 1", got)
	}
	if !containsConflict(report.Conflicts, "runs", "01a10000-0000-7000-8000-0000000000c2") {
		t.Errorf("conflicts = %+v, want the incoming active run", report.Conflicts)
	}
	if report.Tables["run_leases"].Skipped != 1 {
		t.Errorf("lease counts = %+v, want the skipped run's lease skipped", report.Tables["run_leases"])
	}
}

func TestNewProjectWithKnownNameIsFlagged(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.exec("INSERT INTO projects (id, name, revision, created_at, updated_at) VALUES ('01a10000-0000-7000-8000-0000000000f1', 'ACME-API', 1, ?, ?)",
		stampAt(0), stampAt(0))

	report := target.importBundle(source.export(projectID), false)

	if !containsText(report.Warnings, "same name") {
		t.Errorf("warnings = %q, want a duplicate warning", report.Warnings)
	}
	if got := target.count("SELECT COUNT(*) FROM projects"); got != 2 {
		t.Errorf("projects = %d, want both kept", got)
	}
}

func TestBoundProjectStaysBound(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)
	target.importBundle(source.export(projectID), false)
	target.exec("INSERT INTO project_roots (project_id, canonical_path, path_key, active_at) VALUES (?, '/Users/me/src/acme', '/Users/me/src/acme', ?)", projectID, stampAt(0))

	source.exec("UPDATE projects SET name = 'acme', revision = 2 WHERE id = ?", projectID)
	report := target.importBundle(source.export(projectID), false)

	if got := target.text("SELECT canonical_path FROM project_roots WHERE project_id = ? AND detached_at IS NULL", projectID); got != "/Users/me/src/acme" {
		t.Errorf("root = %q, want the local folder kept", got)
	}
	if got := target.text("SELECT name FROM projects WHERE id = ?", projectID); got != "acme" {
		t.Errorf("name = %q, want the renamed project", got)
	}
	if !report.Projects[0].Bound || containsText(report.Warnings, "no folder") {
		t.Errorf("project = %+v, warnings = %q", report.Projects, report.Warnings)
	}
}

func TestImportRejectsOtherSchemaVersion(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)

	bundle := source.export(projectID)
	bundle.SchemaVersion--

	_, err := target.repo.Import(context.Background(), bundle, testActor, false)
	if code, _ := exchange.ErrorCode(err); code != exchange.CodeIncompatible {
		t.Fatalf("Import() error = %v, want incompatible_bundle", err)
	}
}

func TestImportRejectsUnknownColumns(t *testing.T) {
	source := newDevice(t)
	source.seedProject("acme-api")
	target := newDevice(t)

	bundle := source.export(projectID)
	bundle.Tables["tasks"][0]["priority"] = "high"

	_, err := target.repo.Import(context.Background(), bundle, testActor, false)
	if err == nil || !strings.Contains(err.Error(), "unknown column") {
		t.Fatalf("Import() error = %v, want an unknown column error", err)
	}
	if got := target.count("SELECT COUNT(*) FROM projects"); got != 0 {
		t.Errorf("projects = %d, want the failed import rolled back", got)
	}
}

func containsText(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}

	return false
}

func containsConflict(conflicts []exchange.Conflict, table, id string) bool {
	for _, conflict := range conflicts {
		if conflict.Table == table && conflict.ID == id {
			return true
		}
	}

	return false
}

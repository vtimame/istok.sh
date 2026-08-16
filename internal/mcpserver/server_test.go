package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	contextapp "s26.dev/istok-cli/internal/application/context"
	runapp "s26.dev/istok-cli/internal/application/run"
	runworkflow "s26.dev/istok-cli/internal/application/runworkflow"
	taskapp "s26.dev/istok-cli/internal/application/task"
	"s26.dev/istok-cli/internal/artifactstore"
	"s26.dev/istok-cli/internal/buildinfo"
	contextmodel "s26.dev/istok-cli/internal/context"
	"s26.dev/istok-cli/internal/project"
	runmodel "s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/storage"
	"s26.dev/istok-cli/internal/storage/contextrepo"
	"s26.dev/istok-cli/internal/storage/projectrepo"
	"s26.dev/istok-cli/internal/storage/runrepo"
	"s26.dev/istok-cli/internal/storage/taskrepo"
	"s26.dev/istok-cli/internal/task"
)

func TestHealthToolReturnsTypedStatus(t *testing.T) {
	_, session := newSession(t, Worker, t.TempDir())

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "health", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatal("health result is an error")
	}

	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}

	var status HealthStatus
	if err := json.Unmarshal(encoded, &status); err != nil {
		t.Fatalf("unmarshal typed health status: %v", err)
	}
	if status != (HealthStatus{SchemaVersion: "1", Status: "ok", Version: "test"}) {
		t.Errorf("health status = %+v", status)
	}
}

func TestToolProfilesAnnotationsAndSchemas(t *testing.T) {
	worker, workerSession := newSession(t, Worker, t.TempDir())
	_ = worker
	assertTools(t, workerSession, map[string]toolContract{
		"health":                 {readOnly: true, destructive: false, idempotent: true},
		"project_current":        {readOnly: true, destructive: false, idempotent: true},
		"project_init":           {readOnly: false, destructive: false, idempotent: true},
		"task_create":            {readOnly: false, destructive: false, idempotent: true},
		"task_update":            {readOnly: false, destructive: true, idempotent: true},
		"task_list":              {readOnly: true, destructive: false, idempotent: true},
		"task_show":              {readOnly: true, destructive: false, idempotent: true},
		"task_ready":             {readOnly: true, destructive: false, idempotent: true},
		"task_comment":           {readOnly: false, destructive: false, idempotent: true},
		"task_progress":          {readOnly: false, destructive: false, idempotent: true},
		"task_block":             {readOnly: false, destructive: true, idempotent: true},
		"task_unblock":           {readOnly: false, destructive: true, idempotent: true},
		"task_dependency_add":    {readOnly: false, destructive: false, idempotent: true},
		"task_dependency_remove": {readOnly: false, destructive: true, idempotent: true},
		"context_add":            {readOnly: false, destructive: false, idempotent: true},
		"context_update":         {readOnly: false, destructive: true, idempotent: true},
		"context_list":           {readOnly: true, destructive: false, idempotent: true},
		"context_show":           {readOnly: true, destructive: false, idempotent: true},
		"context_search":         {readOnly: true, destructive: false, idempotent: true},
		"context_package":        {readOnly: true, destructive: false, idempotent: true},
		"task_claim":             {readOnly: false, destructive: false, idempotent: false},
		"run_list":               {readOnly: true, destructive: false, idempotent: true},
		"run_show":               {readOnly: true, destructive: false, idempotent: true},
		"run_exec":               {readOnly: false, destructive: true, idempotent: false},
		"run_validate":           {readOnly: false, destructive: true, idempotent: false},
		"run_heartbeat":          {readOnly: false, destructive: false, idempotent: false},
		"run_artifact_list":      {readOnly: true, destructive: false, idempotent: true},
		"run_artifact_verify":    {readOnly: true, destructive: false, idempotent: true},
		"execution_start":        {readOnly: false, destructive: false, idempotent: false},
		"execution_finish":       {readOnly: false, destructive: false, idempotent: false},
		"validation_record":      {readOnly: false, destructive: false, idempotent: false},
		"run_finish":             {readOnly: false, destructive: false, idempotent: false},
		"task_complete":          {readOnly: false, destructive: false, idempotent: false},
	})

	_, adminSession := newSession(t, Admin, t.TempDir())
	assertTools(t, adminSession, map[string]toolContract{
		"health":                 {readOnly: true, destructive: false, idempotent: true},
		"project_current":        {readOnly: true, destructive: false, idempotent: true},
		"project_init":           {readOnly: false, destructive: false, idempotent: true},
		"project_list":           {readOnly: true, destructive: false, idempotent: true},
		"project_rename":         {readOnly: false, destructive: true, idempotent: true},
		"project_rebind":         {readOnly: false, destructive: true, idempotent: true},
		"project_delete":         {readOnly: false, destructive: true, idempotent: true},
		"project_restore":        {readOnly: false, destructive: false, idempotent: true},
		"task_create":            {readOnly: false, destructive: false, idempotent: true},
		"task_update":            {readOnly: false, destructive: true, idempotent: true},
		"task_list":              {readOnly: true, destructive: false, idempotent: true},
		"task_show":              {readOnly: true, destructive: false, idempotent: true},
		"task_ready":             {readOnly: true, destructive: false, idempotent: true},
		"task_comment":           {readOnly: false, destructive: false, idempotent: true},
		"task_progress":          {readOnly: false, destructive: false, idempotent: true},
		"task_block":             {readOnly: false, destructive: true, idempotent: true},
		"task_unblock":           {readOnly: false, destructive: true, idempotent: true},
		"task_dependency_add":    {readOnly: false, destructive: false, idempotent: true},
		"task_dependency_remove": {readOnly: false, destructive: true, idempotent: true},
		"task_delete":            {readOnly: false, destructive: true, idempotent: true},
		"task_restore":           {readOnly: false, destructive: false, idempotent: true},
		"context_add":            {readOnly: false, destructive: false, idempotent: true},
		"context_update":         {readOnly: false, destructive: true, idempotent: true},
		"context_list":           {readOnly: true, destructive: false, idempotent: true},
		"context_show":           {readOnly: true, destructive: false, idempotent: true},
		"context_search":         {readOnly: true, destructive: false, idempotent: true},
		"context_package":        {readOnly: true, destructive: false, idempotent: true},
		"context_archive":        {readOnly: false, destructive: true, idempotent: true},
		"task_claim":             {readOnly: false, destructive: false, idempotent: false},
		"run_list":               {readOnly: true, destructive: false, idempotent: true},
		"run_show":               {readOnly: true, destructive: false, idempotent: true},
		"run_exec":               {readOnly: false, destructive: true, idempotent: false},
		"run_validate":           {readOnly: false, destructive: true, idempotent: false},
		"run_heartbeat":          {readOnly: false, destructive: false, idempotent: false},
		"run_artifact_list":      {readOnly: true, destructive: false, idempotent: true},
		"run_artifact_verify":    {readOnly: true, destructive: false, idempotent: true},
		"run_recover":            {readOnly: false, destructive: true, idempotent: false},
		"execution_start":        {readOnly: false, destructive: false, idempotent: false},
		"execution_finish":       {readOnly: false, destructive: false, idempotent: false},
		"validation_record":      {readOnly: false, destructive: false, idempotent: false},
		"run_finish":             {readOnly: false, destructive: false, idempotent: false},
		"task_complete":          {readOnly: false, destructive: false, idempotent: false},
	})

	_, supervisorSession := newSession(t, Supervisor, t.TempDir())
	supervisorTools, err := supervisorSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list supervisor tools: %v", err)
	}
	for _, forbidden := range []string{"project_delete", "task_delete", "context_archive"} {
		for _, item := range supervisorTools.Tools {
			if item.Name == forbidden {
				t.Fatalf("supervisor exposes %s", forbidden)
			}
		}
	}
	foundRecover := false
	for _, item := range supervisorTools.Tools {
		if item.Name == "run_recover" {
			foundRecover = true
		}
	}
	if !foundRecover {
		t.Fatal("supervisor does not expose run_recover")
	}
}

func TestWorkerProjectCurrentNotFoundIsStructuredToolError(t *testing.T) {
	_, session := newSession(t, Worker, t.TempDir())
	result := callTool(t, session, "project_current", map[string]any{})
	if !result.IsError {
		t.Fatal("project_current without an initialized root is not a tool error")
	}
	var structured ErrorResult
	decodeStructured(t, result, &structured)
	if structured.SchemaVersion != "1" || structured.Error.Code != string(project.CodeNotFound) {
		t.Fatalf("structured error = %+v", structured)
	}
}

func TestScopedInitAndAdminMutationContracts(t *testing.T) {
	root := t.TempDir()
	_, worker := newSession(t, Worker, root)
	first := callTool(t, worker, "project_init", map[string]any{"name": "scoped"})
	var initialized InitResult
	decodeStructured(t, first, &initialized)
	if !initialized.Created || initialized.Project.Root == nil || initialized.Project.Root.CanonicalPath != root {
		t.Fatalf("first scoped init = %+v", initialized)
	}
	second := callTool(t, worker, "project_init", map[string]any{"name": "ignored"})
	var again InitResult
	decodeStructured(t, second, &again)
	if again.Created || again.Project.ID != initialized.Project.ID {
		t.Fatalf("second scoped init = %+v", again)
	}

	_, admin := newSessionAtDatabase(t, Admin, root, filepath.Join(t.TempDir(), "admin.db"))
	adminInitialized := callTool(t, admin, "project_init", map[string]any{"name": "admin"})
	var value InitResult
	decodeStructured(t, adminInitialized, &value)

	invalid := callTool(t, admin, "project_delete", map[string]any{
		"project_id": value.Project.ID[:len(value.Project.ID)-1], "confirm_project_id": value.Project.ID[:len(value.Project.ID)-1], "expected_revision": value.Project.Revision,
	})
	assertToolCode(t, invalid, project.CodeInvalid)
	upperCase := callTool(t, admin, "project_delete", map[string]any{
		"project_id": strings.ToUpper(value.Project.ID), "confirm_project_id": strings.ToUpper(value.Project.ID), "expected_revision": value.Project.Revision,
	})
	assertToolCode(t, upperCase, project.CodeInvalid)

	wrongConfirmation := callTool(t, admin, "project_delete", map[string]any{
		"project_id": value.Project.ID, "confirm_project_id": "00000000-0000-7000-8000-000000000000", "expected_revision": value.Project.Revision,
	})
	assertToolCode(t, wrongConfirmation, project.CodeInvalid)

	stale := callTool(t, admin, "project_delete", map[string]any{
		"project_id": value.Project.ID, "confirm_project_id": value.Project.ID, "expected_revision": value.Project.Revision + 1,
	})
	assertToolCode(t, stale, project.CodeRevisionConflict)

	deleted := callTool(t, admin, "project_delete", map[string]any{
		"project_id": value.Project.ID, "confirm_project_id": value.Project.ID, "expected_revision": value.Project.Revision,
	})
	var deletedProject Result
	decodeStructured(t, deleted, &deletedProject)
	if deletedProject.Project == nil || deletedProject.Project.DeletedAt == nil {
		t.Fatalf("delete result = %+v", deletedProject)
	}

	restored := callTool(t, admin, "project_restore", map[string]any{
		"project_id": value.Project.ID, "expected_revision": deletedProject.Project.Revision,
	})
	var restoredProject Result
	decodeStructured(t, restored, &restoredProject)
	if restoredProject.Project == nil || restoredProject.Project.DeletedAt != nil {
		t.Fatalf("restore result = %+v", restoredProject)
	}
}

func TestTaskToolsHappyContractAndAdminLifecycle(t *testing.T) {
	root := t.TempDir()
	_, worker := newSession(t, Worker, root)

	missing := callTool(t, worker, "task_list", map[string]any{})
	assertToolCode(t, missing, project.CodeNotFound)

	callTool(t, worker, "project_init", map[string]any{"name": "tasks"})
	emptyReady := callTool(t, worker, "task_ready", map[string]any{})
	var emptyReadyTasks TaskListResult
	decodeStructured(t, emptyReady, &emptyReadyTasks)
	if emptyReadyTasks.Tasks == nil || len(emptyReadyTasks.Tasks) != 0 {
		t.Fatalf("empty ready tasks = %+v", emptyReadyTasks)
	}
	emptyReadyJSON, err := json.Marshal(emptyReady.StructuredContent)
	if err != nil {
		t.Fatalf("marshal empty ready structured content: %v", err)
	}
	if !strings.Contains(string(emptyReadyJSON), `"tasks":[]`) {
		t.Fatalf("empty ready structured JSON = %s", emptyReadyJSON)
	}
	firstID := newTaskID(t)
	secondID := newTaskID(t)
	first := createTask(t, worker, firstID, "first")
	second := createTask(t, worker, secondID, "second")
	if first.Task.ID != firstID || first.Task.Number != 1 || second.Task.Number != 2 {
		t.Fatalf("created tasks = %+v, %+v", first.Task, second.Task)
	}

	list := callTool(t, worker, "task_list", map[string]any{})
	var listed TaskListResult
	decodeStructured(t, list, &listed)
	if listed.SchemaVersion != "1" || len(listed.Tasks) != 2 {
		t.Fatalf("task list = %+v", listed)
	}

	show := callTool(t, worker, "task_show", map[string]any{"task_id": firstID})
	var shown TaskShowResult
	decodeStructured(t, show, &shown)
	if shown.Show == nil || shown.Show.Task.ID != firstID || shown.Show.Task.Number != 1 {
		t.Fatalf("task show = %+v", shown)
	}
	wantActor := task.ActorSnapshot{ID: "mcp-agent", Kind: "agent", Name: "MCP Agent"}
	if len(shown.Show.Events) == 0 || shown.Show.Events[0].Actor != wantActor {
		t.Fatalf("created task actor = %+v, want %+v", shown.Show.Events, wantActor)
	}

	updated := updateTask(t, worker, firstID, first.Task.Revision, "first updated")
	commented := mutateTaskTool(t, worker, "task_comment", firstID, updated.Task.Revision, "comment")
	progressed := mutateTaskTool(t, worker, "task_progress", firstID, commented.Task.Revision, "progress")
	blocked := callTaskResult(t, worker, "task_block", map[string]any{"task_id": firstID, "expected_revision": progressed.Task.Revision, "reason": "waiting"})
	if blocked.Task.Status != task.StatusBlocked {
		t.Fatalf("blocked task = %+v", blocked.Task)
	}
	unblocked := callTaskResult(t, worker, "task_unblock", map[string]any{"task_id": firstID, "expected_revision": blocked.Task.Revision, "note": "ready"})
	if unblocked.Task.Status != task.StatusOpen {
		t.Fatalf("unblocked task = %+v", unblocked.Task)
	}

	dependent := callTaskResult(t, worker, "task_dependency_add", map[string]any{"blocker_task_id": firstID, "blocked_task_id": secondID, "expected_revision": second.Task.Revision})
	ready := callTool(t, worker, "task_ready", map[string]any{})
	var readyTasks TaskListResult
	decodeStructured(t, ready, &readyTasks)
	if len(readyTasks.Tasks) != 1 || readyTasks.Tasks[0].ID != firstID {
		t.Fatalf("ready tasks = %+v", readyTasks)
	}
	removed := callTaskResult(t, worker, "task_dependency_remove", map[string]any{"blocker_task_id": firstID, "blocked_task_id": secondID, "expected_revision": dependent.Task.Revision})
	if removed.Task.ID != secondID {
		t.Fatalf("dependency remove task = %+v", removed.Task)
	}

	stale := callTool(t, worker, "task_update", map[string]any{"task_id": firstID, "expected_revision": first.Task.Revision, "title": "stale"})
	assertTaskToolCode(t, stale, task.CodeRevisionConflict)
	invalid := callTool(t, worker, "task_show", map[string]any{"task_id": strings.ToUpper(firstID)})
	assertTaskToolCode(t, invalid, task.CodeInvalid)

	_, admin := newSessionAtDatabase(t, Admin, root, filepath.Join(t.TempDir(), "admin.db"))
	callTool(t, admin, "project_init", map[string]any{"name": "admin tasks"})
	adminID := newTaskID(t)
	adminTask := createTask(t, admin, adminID, "admin")
	deleted := callTaskResult(t, admin, "task_delete", map[string]any{"task_id": adminID, "expected_revision": adminTask.Task.Revision})
	if deleted.Task.DeletedAt == nil {
		t.Fatalf("deleted task = %+v", deleted.Task)
	}
	notVisible := callTool(t, admin, "task_show", map[string]any{"task_id": adminID})
	assertTaskToolCode(t, notVisible, task.CodeNotFound)
	restored := callTaskResult(t, admin, "task_restore", map[string]any{"task_id": adminID, "expected_revision": deleted.Task.Revision})
	if restored.Task.DeletedAt != nil {
		t.Fatalf("restored task = %+v", restored.Task)
	}
}

func TestTaskToolsAreProjectScopedAndShareApplicationState(t *testing.T) {
	database := filepath.Join(t.TempDir(), "scoped.db")
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	_, firstSession := newSessionAtDatabase(t, Worker, firstRoot, database)

	firstProject := initProject(t, firstSession, "first")
	firstID := newTaskID(t)
	firstTask := createTask(t, firstSession, firstID, "first")
	if firstTask.Task.Number != 1 {
		t.Fatalf("first task number = %d, want 1", firstTask.Task.Number)
	}

	_, secondSession := newSessionAtDatabase(t, Worker, secondRoot, database)
	secondProject := initProject(t, secondSession, "second")
	secondID := newTaskID(t)
	secondTask := createTask(t, secondSession, secondID, "second")
	if secondTask.Task.Number != 1 || firstProject.ID == secondProject.ID || firstTask.Task.ID == secondTask.Task.ID {
		t.Fatalf("scoped task identities = first=%+v second=%+v", firstTask.Task, secondTask.Task)
	}

	crossProject := callTool(t, secondSession, "task_show", map[string]any{"task_id": firstID})
	assertTaskToolCode(t, crossProject, task.CodeNotFound)

	show := readTaskWithApplication(t, database, firstProject.ID, firstID)
	if show.Task.ID != firstID || show.Task.Number != 1 || show.Task.ProjectID != firstProject.ID {
		t.Fatalf("application task read = %+v", show.Task)
	}
}

func TestContextToolsAreScopedAndBuildPackage(t *testing.T) {
	database := filepath.Join(t.TempDir(), "context.db")
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	_, firstSession := newSessionAtDatabase(t, Worker, firstRoot, database)
	initProject(t, firstSession, "first")

	added := callTool(t, firstSession, "context_add", map[string]any{
		"title": "Auth decision",
		"body":  "Use signed update archives.",
		"kind":  "decision",
		"tags":  []string{"security"},
	})
	var created ContextResult
	decodeStructured(t, added, &created)
	if created.Context == nil || created.Context.ProjectID == "" || created.Context.Kind != contextmodel.KindDecision {
		t.Fatalf("created context = %+v", created)
	}

	listed := callTool(t, firstSession, "context_list", map[string]any{})
	var list ContextListResult
	decodeStructured(t, listed, &list)
	if len(list.Contexts) != 1 || list.Contexts[0].ID != created.Context.ID {
		t.Fatalf("context list = %+v", list)
	}

	searched := callTool(t, firstSession, "context_search", map[string]any{"query": "signed"})
	var search ContextListResult
	decodeStructured(t, searched, &search)
	if len(search.Contexts) != 1 || search.Contexts[0].ID != created.Context.ID {
		t.Fatalf("context search = %+v", search)
	}

	packaged := callTool(t, firstSession, "context_package", map[string]any{})
	var contextPackage ContextPackageResult
	decodeStructured(t, packaged, &contextPackage)
	if contextPackage.Package == nil || len(contextPackage.Package.Records) != 1 || contextPackage.Package.Records[0].ContentHash == "" {
		t.Fatalf("context package = %+v", contextPackage)
	}

	title := "Updated auth decision"
	updated := callTool(t, firstSession, "context_update", map[string]any{"context_id": created.Context.ID, "expected_revision": created.Context.Revision, "title": title})
	var updatedContext ContextResult
	decodeStructured(t, updated, &updatedContext)
	if updatedContext.Context == nil || updatedContext.Context.Title != title || updatedContext.Context.Revision != created.Context.Revision+1 {
		t.Fatalf("updated context = %+v", updatedContext)
	}

	_, secondSession := newSessionAtDatabase(t, Worker, secondRoot, database)
	initProject(t, secondSession, "second")
	crossProject := callTool(t, secondSession, "context_show", map[string]any{"context_id": created.Context.ID})
	assertContextToolCode(t, crossProject, contextmodel.CodeNotFound)

	_, adminSession := newSessionAtDatabase(t, Admin, firstRoot, database)
	archived := callTool(t, adminSession, "context_archive", map[string]any{"context_id": created.Context.ID, "expected_revision": updatedContext.Context.Revision})
	var archivedContext ContextResult
	decodeStructured(t, archived, &archivedContext)
	if archivedContext.Context == nil || archivedContext.Context.DeletedAt == nil {
		t.Fatalf("archived context = %+v", archivedContext)
	}
}

func TestRunToolsLifecycleAndCompletion(t *testing.T) {
	_, worker := newSession(t, Worker, t.TempDir())

	project := initProject(t, worker, "runner")
	taskID := newTaskID(t)
	created := createTask(t, worker, taskID, "release workflow")

	added := callTool(t, worker, "context_add", map[string]any{
		"title": "Context snapshot for run",
		"body":  "Context that should be captured in run snapshot.",
	})
	var contextAdded ContextResult
	decodeStructured(t, added, &contextAdded)
	if contextAdded.Context == nil || contextAdded.Context.ProjectID != project.ID {
		t.Fatalf("context add = %+v", contextAdded)
	}

	emptyList := callTool(t, worker, "run_list", map[string]any{})
	var emptyRuns RunListResult
	decodeStructured(t, emptyList, &emptyRuns)
	if len(emptyRuns.Runs) != 0 || emptyRuns.Runs == nil {
		t.Fatalf("empty run list = %+v", emptyRuns)
	}

	claimed := callTool(t, worker, "task_claim", map[string]any{
		"task_id": created.Task.ID,
	})
	var claimedRun RunResult
	decodeStructured(t, claimed, &claimedRun)
	if claimedRun.SchemaVersion != "1" || claimedRun.Run == nil || claimedRun.Run.TaskID != created.Task.ID {
		t.Fatalf("run claim = %+v", claimedRun)
	}

	listed := callTool(t, worker, "run_list", map[string]any{"task_id": created.Task.ID})
	var runListResult RunListResult
	decodeStructured(t, listed, &runListResult)
	if len(runListResult.Runs) != 1 || runListResult.Runs[0].ID != claimedRun.Run.ID {
		t.Fatalf("run list = %+v", runListResult)
	}

	show := callTool(t, worker, "run_show", map[string]any{"run_id": claimedRun.Run.ID})
	var shown RunShowResult
	decodeStructured(t, show, &shown)
	if shown.Show == nil || shown.Show.Snapshot.ProjectID != project.ID {
		t.Fatalf("run show = %+v", shown)
	}
	if shown.Show.Executions == nil || shown.Show.Validations == nil {
		t.Fatalf("run show arrays must be initialized = %+v", shown)
	}

	started := callTool(t, worker, "execution_start", map[string]any{
		"run_id":   claimedRun.Run.ID,
		"lease_id": claimedRun.Run.LeaseID,
		"argv":     []string{"go", "test", "./..."},
		"cwd":      t.TempDir(),
	})
	var execution ExecutionResult
	decodeStructured(t, started, &execution)
	if execution.Execution == nil {
		t.Fatalf("execution start = %+v", execution)
	}

	finished := callTool(t, worker, "execution_finish", map[string]any{
		"run_id":            claimedRun.Run.ID,
		"execution_id":      execution.Execution.ID,
		"expected_revision": execution.Execution.Revision,
		"status":            runmodel.ExecutionSucceeded,
		"exit_code":         0,
		"duration_ms":       100,
		"signal":            "",
		"timed_out":         false,
	})
	var finishedExecution ExecutionResult
	decodeStructured(t, finished, &finishedExecution)
	if finishedExecution.Execution == nil || finishedExecution.Execution.Status != runmodel.ExecutionSucceeded {
		t.Fatalf("execution finish = %+v", finishedExecution)
	}

	errRunFinish := callTool(t, worker, "run_finish", map[string]any{
		"run_id":            claimedRun.Run.ID,
		"lease_id":          claimedRun.Run.LeaseID,
		"expected_revision": claimedRun.Run.Revision,
		"status":            runmodel.StatusSucceeded,
		"result_summary":    "ready",
	})
	assertRunToolCode(t, errRunFinish, runmodel.CodeEvidenceRequired)

	validation := callTool(t, worker, "validation_record", map[string]any{
		"run_id":       claimedRun.Run.ID,
		"execution_id": execution.Execution.ID,
		"source":       runmodel.ValidationSourceAttested,
		"status":       runmodel.ValidationStatusPassed,
		"command":      "go test ./...",
		"summary":      "tests passed",
	})
	var validated ValidationResult
	decodeStructured(t, validation, &validated)
	if validated.Validation == nil {
		t.Fatalf("validation record = %+v", validated)
	}

	finishedRun := callTool(t, worker, "run_finish", map[string]any{
		"run_id":            claimedRun.Run.ID,
		"lease_id":          claimedRun.Run.LeaseID,
		"expected_revision": claimedRun.Run.Revision,
		"status":            runmodel.StatusSucceeded,
		"result_summary":    "run succeeded",
	})
	var doneRun RunResult
	decodeStructured(t, finishedRun, &doneRun)
	if doneRun.Run == nil || doneRun.Run.Status != runmodel.StatusSucceeded {
		t.Fatalf("run finish = %+v", doneRun)
	}

	completedTask := callTool(t, worker, "task_complete", map[string]any{
		"task_id":                created.Task.ID,
		"expected_task_revision": created.Task.Revision,
		"run_id":                 doneRun.Run.ID,
		"validation_id":          validated.Validation.ID,
		"note":                   "task completed",
	})
	var completed CompletionResult
	decodeStructured(t, completedTask, &completed)
	if completed.Task == nil || completed.Task.Status != task.StatusDone {
		t.Fatalf("task complete = %+v", completed)
	}
}

func TestRunToolsScopeAndEvidenceConstraints(t *testing.T) {
	database := filepath.Join(t.TempDir(), "run-scope.db")
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()

	_, first := newSessionAtDatabase(t, Worker, firstRoot, database)
	initProject(t, first, "first")

	firstTask := createTask(t, first, newTaskID(t), "first")
	firstClaimed := callTool(t, first, "task_claim", map[string]any{"task_id": firstTask.Task.ID})
	var firstRun RunResult
	decodeStructured(t, firstClaimed, &firstRun)
	if firstRun.Run == nil {
		t.Fatalf("first run claim = %+v", firstRun)
	}

	firstStarted := callTool(t, first, "execution_start", map[string]any{
		"run_id":   firstRun.Run.ID,
		"lease_id": firstRun.Run.LeaseID,
		"argv":     []string{"go", "test"},
		"cwd":      t.TempDir(),
	})
	var firstExecution ExecutionResult
	decodeStructured(t, firstStarted, &firstExecution)
	if firstExecution.Execution == nil {
		t.Fatalf("first execution start = %+v", firstExecution)
	}

	_, second := newSessionAtDatabase(t, Worker, secondRoot, database)
	_ = initProject(t, second, "second")

	crossProjectShow := callTool(t, second, "run_show", map[string]any{"run_id": firstRun.Run.ID})
	assertRunToolCode(t, crossProjectShow, runmodel.CodeNotFound)

	secondTask := createTask(t, second, newTaskID(t), "second")
	secondClaimed := callTool(t, second, "task_claim", map[string]any{"task_id": secondTask.Task.ID})
	var secondRun RunResult
	decodeStructured(t, secondClaimed, &secondRun)
	if secondRun.Run == nil {
		t.Fatalf("second run claim = %+v", secondRun)
	}

	list := callTool(t, second, "run_list", map[string]any{})
	var secondRunList RunListResult
	decodeStructured(t, list, &secondRunList)
	if len(secondRunList.Runs) != 1 || secondRunList.Runs[0].ID != secondRun.Run.ID {
		t.Fatalf("second project run list = %+v", secondRunList)
	}

	crossProjectExecution := callTool(t, second, "execution_finish", map[string]any{
		"run_id":            firstRun.Run.ID,
		"execution_id":      firstExecution.Execution.ID,
		"expected_revision": firstExecution.Execution.Revision,
		"status":            runmodel.ExecutionFailed,
		"exit_code":         1,
		"timed_out":         false,
		"signal":            "",
	})
	assertRunToolCode(t, crossProjectExecution, runmodel.CodeNotFound)
}

func TestManagedRunLifecycleAndArtifacts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)

	_, session := newSession(t, Worker, root)
	initProject(t, session, "managed")
	created := createTask(t, session, newTaskID(t), "managed validation")

	claimed := callTool(t, session, "task_claim", map[string]any{"task_id": created.Task.ID})
	var claimedRun RunResult
	decodeStructured(t, claimed, &claimedRun)

	heartbeat := callTool(t, session, "run_heartbeat", map[string]any{"run_id": claimedRun.Run.ID, "lease_id": claimedRun.Run.LeaseID})
	var alive RunResult
	decodeStructured(t, heartbeat, &alive)
	if alive.Run == nil || alive.Run.LeaseID != claimedRun.Run.LeaseID {
		t.Fatalf("heartbeat = %+v", alive)
	}

	executed := callTool(t, session, "run_exec", map[string]any{"run_id": claimedRun.Run.ID, "lease_id": claimedRun.Run.LeaseID, "argv": []string{"/bin/sh", "-c", "test -n \"$HOME\" && printf exec"}})
	if executed.IsError {
		t.Fatalf("run_exec error = %+v", executed)
	}
	var managedExecution ManagedExecutionResult
	decodeStructured(t, executed, &managedExecution)
	if managedExecution.Execution == nil || managedExecution.Execution.Status != runmodel.ExecutionSucceeded || managedExecution.Validation != nil || len(managedExecution.Artifacts) != 2 {
		t.Fatalf("managed execution = %+v", managedExecution)
	}

	validated := callTool(t, session, "run_validate", map[string]any{"run_id": claimedRun.Run.ID, "lease_id": claimedRun.Run.LeaseID, "argv": []string{"/bin/sh", "-c", "printf stdout; printf stderr >&2"}})
	if validated.IsError {
		t.Fatalf("run_validate error = %+v", validated)
	}
	var managed ManagedExecutionResult
	decodeStructured(t, validated, &managed)
	if managed.SchemaVersion != "1" || managed.Execution == nil || managed.Validation == nil || managed.Validation.Status != runmodel.ValidationStatusPassed || len(managed.Artifacts) != 2 {
		t.Fatalf("managed validation = %+v", managed)
	}

	listed := callTool(t, session, "run_artifact_list", map[string]any{"run_id": claimedRun.Run.ID})
	var artifacts ArtifactListResult
	decodeStructured(t, listed, &artifacts)
	if artifacts.SchemaVersion != "1" || len(artifacts.Artifacts) != 4 {
		t.Fatalf("artifact list = %+v", artifacts)
	}
	verified := callTool(t, session, "run_artifact_verify", map[string]any{"run_id": claimedRun.Run.ID, "artifact_id": artifacts.Artifacts[0].ID})
	var verification ArtifactVerifyResult
	decodeStructured(t, verified, &verification)
	if verification.SchemaVersion != "1" || !verification.Verified || verification.Artifact == nil {
		t.Fatalf("artifact verification = %+v", verification)
	}

	finished := callTool(t, session, "run_finish", map[string]any{"run_id": claimedRun.Run.ID, "lease_id": claimedRun.Run.LeaseID, "expected_revision": claimedRun.Run.Revision, "status": runmodel.StatusSucceeded, "result_summary": "validation passed"})
	var done RunResult
	decodeStructured(t, finished, &done)
	if done.Run == nil || done.Run.Status != runmodel.StatusSucceeded {
		t.Fatalf("run finish = %+v", done)
	}

	completed := callTool(t, session, "task_complete", map[string]any{"task_id": created.Task.ID, "expected_task_revision": created.Task.Revision, "run_id": done.Run.ID, "validation_id": managed.Validation.ID, "note": "done"})
	var completion CompletionResult
	decodeStructured(t, completed, &completion)
	if completion.Task == nil || completion.Task.Status != task.StatusDone {
		t.Fatalf("task completion = %+v", completion)
	}
}

func TestManagedExecutionInputRequiresPositiveBoundedOptions(t *testing.T) {
	runID := newTaskID(t)
	leaseID := newTaskID(t)
	zero := int64(0)
	negative := int64(-1)
	overflow := maxDurationMilliseconds + 1

	for name, input := range map[string]managedExecutionInput{
		"zero timeout":      {RunID: runID, LeaseID: leaseID, Argv: []string{"true"}, TimeoutMS: &zero},
		"negative grace":    {RunID: runID, LeaseID: leaseID, Argv: []string{"true"}, TerminationGraceMS: &negative},
		"overflow timeout":  {RunID: runID, LeaseID: leaseID, Argv: []string{"true"}, TimeoutMS: &overflow},
		"zero output limit": {RunID: runID, LeaseID: leaseID, Argv: []string{"true"}, OutputLimit: &zero},
	} {
		t.Run(name, func(t *testing.T) {
			if code := runmodel.ErrorCode(input.validate()); code != runmodel.CodeInvalid {
				t.Fatalf("validate error code = %q, want %q", code, runmodel.CodeInvalid)
			}
		})
	}
}

func TestActorOwnershipAndSupervisorRecovery(t *testing.T) {
	database := filepath.Join(t.TempDir(), "actors.db")
	root := t.TempDir()
	_, first := newSessionAtDatabaseWithActor(t, Worker, root, database, "first-agent")
	initProject(t, first, "actors")
	created := createTask(t, first, newTaskID(t), "owned")
	claimed := callTool(t, first, "task_claim", map[string]any{"task_id": created.Task.ID})
	var owned RunResult
	decodeStructured(t, claimed, &owned)

	_, second := newSessionAtDatabaseWithActor(t, Worker, root, database, "second-agent")
	stolenHeartbeat := callTool(t, second, "run_heartbeat", map[string]any{"run_id": owned.Run.ID, "lease_id": owned.Run.LeaseID})
	if !stolenHeartbeat.IsError {
		t.Fatal("second actor unexpectedly heartbeated owned run")
	}
	stolenFinish := callTool(t, second, "run_finish", map[string]any{"run_id": owned.Run.ID, "lease_id": owned.Run.LeaseID, "expected_revision": owned.Run.Revision, "status": runmodel.StatusCancelled, "result_summary": "stolen"})
	if !stolenFinish.IsError {
		t.Fatal("second actor unexpectedly finished owned run")
	}

	_, supervisor := newSessionAtDatabaseWithActor(t, Supervisor, root, database, "supervisor-agent")
	recovered := callTool(t, supervisor, "run_recover", map[string]any{"run_id": owned.Run.ID, "force": true, "reason": "supervisor takeover"})
	var recovery RunResult
	decodeStructured(t, recovered, &recovery)
	if recovery.Run == nil || recovery.Run.LeaseID == owned.Run.LeaseID {
		t.Fatalf("recovery = %+v", recovery)
	}
}

func assertRunToolCode(t *testing.T, result *mcp.CallToolResult, want runmodel.Code) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("tool result = %+v, want error %q", result, want)
	}
	var payload ErrorResult
	decodeStructured(t, result, &payload)
	if payload.SchemaVersion != "1" || payload.Error.Code != string(want) {
		t.Fatalf("tool error = %+v, want %q", payload, want)
	}
}

func initProject(t *testing.T, session *mcp.ClientSession, name string) project.Project {
	t.Helper()
	result := callTool(t, session, "project_init", map[string]any{"name": name})
	var initialized InitResult
	decodeStructured(t, result, &initialized)
	if initialized.Project == nil {
		t.Fatalf("initialized project = %+v", initialized)
	}
	return *initialized.Project
}

func readTaskWithApplication(t *testing.T, database, projectID, taskID string) task.Show {
	t.Helper()
	var service *taskapp.Service
	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: database}),
		fx.Provide(taskrepo.New),
		fx.Provide(func(repository *taskrepo.Repository) taskapp.Repository { return repository }),
		fx.Provide(func() taskapp.ActiveRunInspector { return taskapp.NoActiveRuns{} }),
		fx.Provide(taskapp.NewService),
		fx.Invoke(func(value *taskapp.Service) { service = value }),
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start application reader: %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	value, err := service.Show(context.Background(), task.Selector{ProjectID: projectID, ID: taskID}, false)
	if err != nil {
		t.Fatalf("application task show: %v", err)
	}
	return value
}

func createTask(t *testing.T, session *mcp.ClientSession, id, title string) TaskResult {
	t.Helper()
	return callTaskResult(t, session, "task_create", map[string]any{"task_id": id, "title": title})
}

func updateTask(t *testing.T, session *mcp.ClientSession, id string, revision int64, title string) TaskResult {
	t.Helper()
	return callTaskResult(t, session, "task_update", map[string]any{"task_id": id, "expected_revision": revision, "title": title})
}

func mutateTaskTool(t *testing.T, session *mcp.ClientSession, name, id string, revision int64, body string) TaskResult {
	t.Helper()
	return callTaskResult(t, session, name, map[string]any{"task_id": id, "expected_revision": revision, "body": body})
}

func callTaskResult(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) TaskResult {
	t.Helper()
	result := callTool(t, session, name, arguments)
	if result.IsError {
		t.Fatalf("%s error result = %+v", name, result)
	}
	var value TaskResult
	decodeStructured(t, result, &value)
	if value.SchemaVersion != "1" || value.Task == nil {
		t.Fatalf("%s structured result = %+v", name, value)
	}
	return value
}

func newTaskID(t *testing.T) string {
	t.Helper()
	id, err := task.NewID()
	if err != nil {
		t.Fatalf("new task id: %v", err)
	}
	return id
}

type toolContract struct {
	readOnly    bool
	destructive bool
	idempotent  bool
}

func assertTools(t *testing.T, session *mcp.ClientSession, want map[string]toolContract) {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	gotNames := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		gotNames = append(gotNames, tool.Name)
		contract, ok := want[tool.Name]
		if !ok {
			continue
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != contract.readOnly || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != contract.destructive || tool.Annotations.IdempotentHint != contract.idempotent || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Fatalf("tool %q annotations = %+v", tool.Name, tool.Annotations)
		}
		assertScopedSchema(t, tool)
	}
	sort.Strings(gotNames)
	wantNames := make([]string, 0, len(want))
	for name := range want {
		wantNames = append(wantNames, name)
	}
	sort.Strings(wantNames)
	if strings.Join(gotNames, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("tool names = %v, want %v", gotNames, wantNames)
	}
}

func assertScopedSchema(t *testing.T, tool *mcp.Tool) {
	t.Helper()
	encoded, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal %s input schema: %v", tool.Name, err)
	}
	schema := string(encoded)
	for _, forbidden := range []string{"path", "root", "actor", "actor_id", "actor_name"} {
		if strings.Contains(strings.ToLower(schema), forbidden) {
			t.Fatalf("%s exposes caller-controlled %q in input schema: %s", tool.Name, forbidden, schema)
		}
	}
	if strings.HasPrefix(tool.Name, "task_") && strings.Contains(schema, "project") {
		t.Fatalf("%s exposes caller-controlled project scope in input schema: %s", tool.Name, schema)
	}
	if tool.Name == "project_rebind" || tool.Name == "project_restore" {
		if strings.Contains(schema, "\"name\"") {
			t.Fatalf("%s schema unexpectedly includes name: %s", tool.Name, schema)
		}
	}
}

func newSession(t *testing.T, profile Profile, root string) (*mcp.Server, *mcp.ClientSession) {
	t.Helper()
	return newSessionAtDatabase(t, profile, root, filepath.Join(t.TempDir(), "istok.db"))
}

func newSessionAtDatabase(t *testing.T, profile Profile, root, database string) (*mcp.Server, *mcp.ClientSession) {
	return newSessionAtDatabaseWithActor(t, profile, root, database, "mcp-agent")
}

func newSessionAtDatabaseWithActor(t *testing.T, profile Profile, root, database, actorID string) (*mcp.Server, *mcp.ClientSession) {
	t.Helper()
	var server *mcp.Server
	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: database}),
		fx.Provide(projectrepo.New),
		fx.Provide(func(repository *projectrepo.Repository) project.Repository { return repository }),
		fx.Provide(project.NewService),
		fx.Provide(taskrepo.New),
		fx.Provide(func(repository *taskrepo.Repository) taskapp.Repository { return repository }),
		fx.Provide(contextrepo.New),
		fx.Provide(func(repository *contextrepo.Repository) contextapp.Repository { return repository }),
		fx.Provide(contextapp.NewService),
		fx.Provide(runrepo.New),
		fx.Provide(func(repository *runrepo.Repository) runapp.Repository { return repository }),
		fx.Provide(func(repository *runrepo.Repository) taskapp.ActiveRunInspector { return repository }),
		fx.Provide(taskapp.NewService),
		fx.Provide(func(repository *taskrepo.Repository) runapp.TaskResolver { return repository }),
		fx.Provide(func(service *contextapp.Service) runapp.ContextBuilder { return service }),
		fx.Provide(runapp.NewService),
		fx.Provide(func() (*artifactstore.Store, error) {
			return artifactstore.New(filepath.Join(filepath.Dir(database), "artifacts"))
		}),
		fx.Provide(runworkflow.NewService),
		fx.Provide(func(db *sql.DB) HealthChecker { return db }),
		fx.Provide(func(health HealthChecker, service *project.Service, tasks *taskapp.Service, contexts *contextapp.Service, runs *runapp.Service, workflow *runworkflow.Service) (*mcp.Server, error) {
			return New(health, service, tasks, contexts, runs, workflow, buildinfo.Info{Version: "test"}, Config{Profile: profile, Root: root, ActorID: actorID, ActorName: "MCP Agent"})
		}),
		fx.Invoke(func(value *mcp.Server) { server = value }),
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start migrated test database: %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return server, session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func decodeStructured(t *testing.T, result *mcp.CallToolResult, into any) {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(encoded, into); err != nil {
		t.Fatalf("unmarshal structured content %s: %v", encoded, err)
	}
}

func assertToolCode(t *testing.T, result *mcp.CallToolResult, want project.Code) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("tool result = %+v, want error %q", result, want)
	}
	var payload ErrorResult
	decodeStructured(t, result, &payload)
	if payload.SchemaVersion != "1" || payload.Error.Code != string(want) {
		t.Fatalf("tool error = %+v, want %q", payload, want)
	}
}

func assertTaskToolCode(t *testing.T, result *mcp.CallToolResult, want task.Code) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("tool result = %+v, want error %q", result, want)
	}
	var payload ErrorResult
	decodeStructured(t, result, &payload)
	if payload.SchemaVersion != "1" || payload.Error.Code != string(want) {
		t.Fatalf("tool error = %+v, want %q", payload, want)
	}
}

func assertContextToolCode(t *testing.T, result *mcp.CallToolResult, want contextmodel.Code) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("tool result = %+v, want error %q", result, want)
	}
	var payload ErrorResult
	decodeStructured(t, result, &payload)
	if payload.SchemaVersion != "1" || payload.Error.Code != string(want) {
		t.Fatalf("tool error = %+v, want %q", payload, want)
	}
}

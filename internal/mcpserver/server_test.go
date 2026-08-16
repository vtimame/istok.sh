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

	"s26.dev/istok-cli/internal/buildinfo"
	"s26.dev/istok-cli/internal/project"
	"s26.dev/istok-cli/internal/storage"
	"s26.dev/istok-cli/internal/storage/projectrepo"
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
		"health":          {readOnly: true, destructive: false, idempotent: true},
		"project_current": {readOnly: true, destructive: false, idempotent: true},
		"project_init":    {readOnly: false, destructive: false, idempotent: true},
	})

	_, adminSession := newSession(t, Admin, t.TempDir())
	assertTools(t, adminSession, map[string]toolContract{
		"health":          {readOnly: true, destructive: false, idempotent: true},
		"project_current": {readOnly: true, destructive: false, idempotent: true},
		"project_init":    {readOnly: false, destructive: false, idempotent: true},
		"project_list":    {readOnly: true, destructive: false, idempotent: true},
		"project_rename":  {readOnly: false, destructive: true, idempotent: true},
		"project_rebind":  {readOnly: false, destructive: true, idempotent: true},
		"project_delete":  {readOnly: false, destructive: true, idempotent: true},
		"project_restore": {readOnly: false, destructive: false, idempotent: true},
	})
}

func TestWorkerProjectCurrentNotFoundIsStructuredToolError(t *testing.T) {
	_, session := newSession(t, Worker, t.TempDir())
	result := callTool(t, session, "project_current", map[string]any{})
	if !result.IsError {
		t.Fatal("project_current without an initialized root is not a tool error")
	}
	var structured ErrorResult
	decodeStructured(t, result, &structured)
	if structured.SchemaVersion != "1" || structured.Error.Code != project.CodeNotFound {
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
	for _, forbidden := range []string{"path", "root", "cwd"} {
		if strings.Contains(strings.ToLower(schema), forbidden) {
			t.Fatalf("%s exposes caller-controlled %q in input schema: %s", tool.Name, forbidden, schema)
		}
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
	t.Helper()
	var server *mcp.Server
	app := fx.New(
		fx.NopLogger,
		storage.Module(storage.Config{Path: database}),
		fx.Provide(projectrepo.New),
		fx.Provide(func(repository *projectrepo.Repository) project.Repository { return repository }),
		fx.Provide(project.NewService),
		fx.Provide(func(db *sql.DB, service *project.Service) *mcp.Server {
			return New(db, service, buildinfo.Info{Version: "test"}, Config{Profile: profile, Root: root})
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
	if payload.SchemaVersion != "1" || payload.Error.Code != want {
		t.Fatalf("tool error = %+v, want %q", payload, want)
	}
}

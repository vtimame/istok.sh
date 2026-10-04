package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPWorkflowOverStdio(t *testing.T) {
	env := newEnvironment(t, true)
	var stderr bytes.Buffer
	command := exec.Command(istokBinary, "mcp", "--actor-id", "e2e-agent", "--actor-name", "E2E Agent")
	command.Dir = env.root
	command.Env = env.env
	command.Stderr = &stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = session.Close()
		}
	})

	project := mcpObject(t, mcpCall(t, session, "project_init", map[string]any{"name": "mcp e2e"}), "project")
	if project["id"] == "" {
		t.Fatalf("project init = %#v", project)
	}
	contextRecord := mcpObject(t, mcpCall(t, session, "context_add", map[string]any{"title": "MCP context", "body": "captured in run"}), "context")
	if contextRecord["id"] == "" {
		t.Fatalf("context add = %#v", contextRecord)
	}
	knowledgeResult := mcpCall(t, session, "knowledge_create", map[string]any{
		"title": "MCP knowledge", "summary": "Bounded run briefing", "body": strings.Repeat("pull-only body ", 100), "kind": "architecture",
	})
	knowledgeItem := mcpObject(t, knowledgeResult, "knowledge")
	knowledgeID := stringValue(t, knowledgeItem, "id")
	reviewed := mcpObject(t, mcpCall(t, session, "knowledge_review", map[string]any{"knowledge_id": knowledgeID, "expected_revision": number(t, knowledgeItem, "revision"), "note": "e2e review"}), "knowledge")
	mcpCall(t, session, "knowledge_promote", map[string]any{"knowledge_id": knowledgeID, "expected_revision": number(t, reviewed, "revision")})
	catalog := mcpStructuredAtVersion(t, mcpCall(t, session, "knowledge_catalog", map[string]any{}), "1")
	items := array(t, catalog, "items")
	if len(items) != 1 || items[0].(map[string]any)["body"] != nil {
		t.Fatalf("knowledge catalog = %#v", catalog)
	}

	taskID := uuid.Must(uuid.NewV7()).String()
	created := mcpObject(t, mcpCall(t, session, "task_create", map[string]any{"task_id": taskID, "title": "MCP workflow"}), "task")
	claimResult := mcpCall(t, session, "task_claim", map[string]any{"task_id": taskID})
	claimed := mcpObjectAtVersion(t, claimResult, "2", "run")
	snapshot := mcpObjectAtVersion(t, claimResult, "2", "snapshot")
	briefing := array(t, snapshot, "knowledge_catalog")
	if len(briefing) != 1 || briefing[0].(map[string]any)["body"] != nil {
		t.Fatalf("knowledge briefing = %#v", snapshot)
	}
	runID, leaseID := stringValue(t, claimed, "id"), stringValue(t, claimed, "lease_id")
	mcpCall(t, session, "run_heartbeat", map[string]any{"run_id": runID, "lease_id": leaseID})

	managed := mcpStructuredAtVersion(t, mcpCall(t, session, "run_validate", map[string]any{"run_id": runID, "lease_id": leaseID, "argv": []string{"/bin/sh", "-c", "printf mcp-out; printf mcp-err >&2"}}), "2")
	validation := object(t, managed, "validation")
	artifacts := array(t, managed, "artifacts")
	if len(artifacts) != 2 {
		t.Fatalf("managed artifacts = %#v", artifacts)
	}
	for _, item := range artifacts {
		artifact := item.(map[string]any)
		verified := mcpStructuredAtVersion(t, mcpCall(t, session, "run_artifact_verify", map[string]any{"run_id": runID, "artifact_id": artifact["id"]}), "2")
		if !boolValue(t, verified, "verified") {
			t.Fatalf("artifact verification = %#v", verified)
		}
	}
	mcpCall(t, session, "run_artifact_list", map[string]any{"run_id": runID})
	mcpCall(t, session, "run_finish", map[string]any{"run_id": runID, "lease_id": leaseID, "expected_revision": number(t, claimed, "revision"), "status": "succeeded", "result_summary": "MCP validation passed"})
	mcpCall(t, session, "task_complete", map[string]any{"task_id": taskID, "expected_task_revision": number(t, created, "revision"), "run_id": runID, "validation_id": validation["id"], "note": "MCP workflow complete"})

	shown := env.runJSON(t, "task", "show", "1", "--json")
	if stringValue(t, object(t, shown, "result", "task", "task"), "status") != "done" {
		t.Fatalf("CLI cannot see MCP task state: %#v", shown)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close MCP session: %v", err)
	}
	closed = true
	if stderr.Len() != 0 {
		t.Fatalf("MCP child stderr = %q", stderr.String())
	}
}

func mcpCall(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("MCP %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("MCP %s tool error: %+v", name, result)
	}
	return result
}

func mcpStructured(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()

	return mcpStructuredAtVersion(t, result, "1")
}

func mcpStructuredAtVersion(t *testing.T, result *mcp.CallToolResult, wantVersion string) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal MCP structured result: %v", err)
	}
	value := decodeJSON(t, string(encoded))
	assertSchemaVersionAt(t, value, wantVersion)
	return value
}

func mcpObject(t *testing.T, result *mcp.CallToolResult, key string) map[string]any {
	t.Helper()
	return object(t, mcpStructured(t, result), key)
}

func mcpObjectAtVersion(t *testing.T, result *mcp.CallToolResult, wantVersion, key string) map[string]any {
	t.Helper()

	return object(t, mcpStructuredAtVersion(t, result, wantVersion), key)
}

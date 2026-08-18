//go:build hardening

package hardening

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConcurrentCLIAndMCP(t *testing.T) {
	e := newEnvironment(t)
	writeCorpus(t, e.root, corpusSize(t))
	e.runJSON(t, "init", "--json")

	var stderr bytes.Buffer
	command := exec.Command(os.Getenv("ISTOK_HARDENING_BINARY"), "mcp", "--actor-id", "hardening-agent", "--actor-name", "Hardening Agent")
	command.Dir, command.Env, command.Stderr = e.root, e.env, &stderr
	ctx, cancel := context.WithTimeout(context.Background(), hardeningTimeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "hardening", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("connect MCP: %v\nstderr:\n%s", err, stderr.String())
	}
	t.Cleanup(func() { _ = session.Close() })

	taskID := uuid.Must(uuid.NewV7()).String()
	created := mcpValue(t, mcpCall(t, ctx, session, "task_create", map[string]any{"task_id": taskID, "title": "HardeningSymbol0000 retrieval"}))
	if stringValue(t, object(t, created, "task"), "id") != taskID {
		t.Fatalf("created task = %#v", created)
	}
	if err := os.WriteFile(filepath.Join(e.root, "concurrent.go"), []byte("package fixture\nfunc ConcurrentHardeningSymbol() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	operations := [][]string{{"search", "ConcurrentHardeningSymbol", "--json"}, {"graph", "symbol", "HardeningSymbol0000", "--json"}, {"graph", "symbol", "ConcurrentHardeningSymbol", "--json"}}
	errs := make(chan error, len(operations)+1)
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_claim", Arguments: map[string]any{"task_id": taskID}})
		if err != nil {
			errs <- fmt.Errorf("MCP task_claim: %w", err)
			return
		}
		if result.IsError {
			errs <- fmt.Errorf("MCP task_claim error: %#v", result)
			return
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			errs <- fmt.Errorf("encode claim response: %w", err)
			return
		}
		value, err := decodeVersioned(string(encoded), "2")
		if err != nil {
			errs <- fmt.Errorf("decode claim response: %w", err)
			return
		}
		run, ok := value["run"].(map[string]any)
		if !ok || run["id"] == "" {
			errs <- fmt.Errorf("claim = %#v", value)
		}
	}()
	for _, args := range operations {
		args := args
		group.Add(1)
		go func() {
			defer group.Done()
			result := e.command(ctx, args...)
			if result.err != nil {
				errs <- fmt.Errorf("istok %q: %w\nstderr:\n%s", args, result.err, result.stderr)
				return
			}
			if _, err := decodeVersioned(result.stdout, "1"); err != nil {
				errs <- fmt.Errorf("istok %q: %w", args, err)
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	runs := mcpValueVersion(t, mcpCall(t, ctx, session, "run_list", map[string]any{"task_id": taskID}), "2")
	items := array(t, runs, "runs")
	if len(items) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	runID := stringValue(t, items[0].(map[string]any), "id")
	show := e.runJSONVersion(t, "2", "run", "show", runID, "--json")
	if len(array(t, object(t, show, "result", "snapshot"), "retrieval")) == 0 {
		t.Fatalf("claim snapshot has no retrieval: %#v", show)
	}
	if len(array(t, object(t, e.runJSON(t, "search", "ConcurrentHardeningSymbol", "--json"), "result"), "results")) == 0 {
		t.Fatal("index unreadable after concurrent operations")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close MCP: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("MCP stderr:\n%s", stderr.String())
	}
}

func mcpCall(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("MCP %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("MCP %s error: %#v", name, result)
	}
	return result
}

func mcpValue(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()

	return mcpValueVersion(t, result, "1")
}

func mcpValueVersion(t *testing.T, result *mcp.CallToolResult, wantVersion string) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return decodeJSONVersion(t, string(encoded), wantVersion)
}

func decodeVersioned(text, wantVersion string) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return nil, err
	}
	if value["schema_version"] != wantVersion {
		return nil, fmt.Errorf("schema_version = %#v, want %s", value["schema_version"], wantVersion)
	}
	return value, nil
}

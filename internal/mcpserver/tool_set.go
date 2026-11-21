package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolSet selects how many tools the server lists. Every tool definition
// costs agent context, so the default core set covers the short workflow and
// the rest stays available behind --tools full.
type ToolSet string

const (
	ToolsCore ToolSet = "core"
	ToolsFull ToolSet = "full"
)

// coreTools is the short workflow: find or start a task, record progress,
// commits, handoffs and checks, close it, save project rules, and read
// knowledge and code.
var coreTools = map[string]bool{
	"project_current":  true,
	"project_init":     true,
	"task_ready":       true,
	"task_list":        true,
	"task_show":        true,
	"task_start":       true,
	"task_progress":    true,
	"task_block":       true,
	"task_handoff":     true,
	"task_commit":      true,
	"task_evidence":    true,
	"run_validate":     true,
	"task_done":        true,
	"run_recover":      true,
	"context_add":      true,
	"context_list":     true,
	"context_update":   true,
	"knowledge_search": true,
	"knowledge_read":   true,
	"search":           true,
}

// profileTools are the tools a profile adds on top of the worker tools. The
// core set keeps them: a profile is chosen on purpose.
var profileTools = map[Profile][]string{
	Supervisor: {"run_abandon"},
	Admin: {
		"run_abandon",
		"project_list", "project_rename", "project_rebind", "project_delete", "project_restore",
		"task_delete", "task_restore", "context_archive",
	},
}

// codeToolUnavailable is returned for a registered tool hidden by the core set.
const codeToolUnavailable = "tool_unavailable"

// coreFilter hides the tools outside the core set from tools/list and rejects
// calls to them with a hint, so a client with a stale tool list learns how to
// get them.
func coreFilter(profile Profile) mcp.Middleware {
	keep := map[string]bool{}
	for name := range coreTools {
		keep[name] = true
	}
	for _, name := range profileTools[profile] {
		keep[name] = true
	}

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if call, ok := request.(*mcp.CallToolRequest); ok && method == "tools/call" && call.Params != nil && !keep[call.Params.Name] {
				return unavailableTool(call.Params.Name), nil
			}

			result, err := next(ctx, method, request)
			if err != nil || method != "tools/list" {
				return result, err
			}

			listed, ok := result.(*mcp.ListToolsResult)
			if !ok {
				return result, err
			}

			visible := make([]*mcp.Tool, 0, len(keep))
			for _, value := range listed.Tools {
				if keep[value.Name] {
					visible = append(visible, value)
				}
			}
			listed.Tools = visible

			return listed, nil
		}
	}
}

func unavailableTool(name string) *mcp.CallToolResult {
	encoded, _ := json.Marshal(ErrorResult{SchemaVersion: "1", Error: ToolError{
		Code:    codeToolUnavailable,
		Message: fmt.Sprintf("tool %q is not available: this server lists only the core tools; start istok mcp with --tools full for the rest", name),
	}})

	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}
}

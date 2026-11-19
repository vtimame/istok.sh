package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vtimame/istok.sh/internal/project"
)

// ProjectSyncer syncs a project with the hub. A device that does not sync is
// not an error; the syncer does nothing.
type ProjectSyncer interface {
	SyncProject(ctx context.Context, projectID string) (bool, error)
	Nudge(projectID string)
	Flush(ctx context.Context)
}

// startSyncTimeout bounds the pull before task_start, so an unreachable hub
// delays the agent only briefly.
const startSyncTimeout = 4 * time.Second

// readTools change nothing worth sending, so they do not trigger a push. A
// heartbeat only extends the lease; the next real write carries it.
var readTools = map[string]bool{
	"health": true, "project_current": true, "project_list": true,
	"task_ready": true, "task_list": true, "task_show": true,
	"run_list": true, "run_show": true, "run_heartbeat": true,
	"run_artifact_list": true, "run_artifact_verify": true,
	"context_list": true, "context_show": true, "context_search": true,
	"context_preview": true, "context_package": true, "context_doctor": true,
	"knowledge_catalog": true, "knowledge_search": true, "knowledge_read": true,
	"search": true, "graph_symbol": true, "graph_neighbors": true, "graph_path": true,
	"index_status": true, "index_rebuild": true,
}

// SyncFilter pulls the project before task_start, so a handoff written on
// another device is in the brief, and pushes shortly after every write.
func SyncFilter(syncer ProjectSyncer, projects *project.Service, root string) mcp.Middleware {
	projectID := func(ctx context.Context) string {
		value, err := projects.Current(ctx, root)
		if err != nil {
			return ""
		}

		return value.ID
	}

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			call, ok := request.(*mcp.CallToolRequest)
			if !ok || method != "tools/call" || call.Params == nil {
				return next(ctx, method, request)
			}

			name := call.Params.Name
			if name == "task_start" {
				if id := projectID(ctx); id != "" {
					pullCtx, cancel := context.WithTimeout(ctx, startSyncTimeout)
					_, _ = syncer.SyncProject(pullCtx, id)
					cancel()
				}
			}

			result, err := next(ctx, method, request)

			if !readTools[name] {
				if id := projectID(ctx); id != "" {
					syncer.Nudge(id)
				}
			}

			return result, err
		}
	}
}

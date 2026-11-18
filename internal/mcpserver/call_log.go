package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/usage"
)

// CallRecorder stores the device-local log of tool calls.
type CallRecorder interface {
	Record(context.Context, usage.Call) error
	Prune(context.Context) error
}

// recordTimeout bounds a log write, so a busy database delays a tool result
// only briefly.
const recordTimeout = 2 * time.Second

// callLog records every tools/call request: which tool, how long it took and
// how it ended. Arguments and results are never stored. A failed write is
// reported to stderr once and never fails the call.
type callLog struct {
	recorder  CallRecorder
	projects  *project.Service
	root      string
	actorID   string
	sessionID string

	projectMu sync.Mutex
	projectID string
	pruneOnce sync.Once
	warnOnce  sync.Once
}

func newCallLog(recorder CallRecorder, projects *project.Service, root, actorID string) (*callLog, error) {
	sessionID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("create MCP session ID: %w", err)
	}

	return &callLog{recorder: recorder, projects: projects, root: root, actorID: actorID, sessionID: sessionID.String()}, nil
}

func (l *callLog) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		call, ok := request.(*mcp.CallToolRequest)
		if !ok || call.Params == nil {
			return next(ctx, method, request)
		}

		started := time.Now()
		result, err := next(ctx, method, request)
		duration := time.Since(started)

		l.record(ctx, call, started, duration, result, err)

		return result, err
	}
}

func (l *callLog) record(ctx context.Context, call *mcp.CallToolRequest, started time.Time, duration time.Duration, result mcp.Result, callErr error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()

	l.pruneOnce.Do(func() { l.warn(l.recorder.Prune(ctx)) })

	id, err := uuid.NewV7()
	if err != nil {
		l.warn(err)
		return
	}

	value := usage.Call{
		ID:        id.String(),
		SessionID: l.sessionID,
		ActorID:   l.actorID,
		ProjectID: l.currentProject(ctx),
		Tool:      call.Params.Name,
		StartedAt: started,
		Duration:  duration,
		Outcome:   usage.OK,
	}

	if client := call.ClientInfo(); client != nil {
		value.ClientName = client.Name
		value.ClientVersion = client.Version
	}

	if code := failureCode(result, callErr); code != "" {
		value.Outcome = usage.Error
		value.ErrorCode = code
	}

	l.warn(l.recorder.Record(ctx, value))
}

// currentProject resolves the project at the server root once. A root that is
// not registered yet is looked up again on later calls, so calls after
// project_init are attributed to the new project.
func (l *callLog) currentProject(ctx context.Context) string {
	l.projectMu.Lock()
	defer l.projectMu.Unlock()

	if l.projectID != "" {
		return l.projectID
	}

	current, err := l.projects.Current(ctx, l.root)
	if err != nil {
		return ""
	}
	l.projectID = current.ID

	return l.projectID
}

func (l *callLog) warn(err error) {
	if err == nil {
		return
	}

	l.warnOnce.Do(func() { fmt.Fprintf(os.Stderr, "istok: MCP call log is unavailable: %v\n", err) })
}

// failureCode returns the error code of a failed call, or "" for a success.
// Tool errors carry a JSON envelope with a code; other failures get a generic
// code.
func failureCode(result mcp.Result, callErr error) string {
	if callErr != nil {
		return "protocol_error"
	}

	value, ok := result.(*mcp.CallToolResult)
	if !ok || value == nil || !value.IsError {
		return ""
	}

	for _, content := range value.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}

		var envelope ErrorResult
		if err := json.Unmarshal([]byte(text.Text), &envelope); err == nil && envelope.Error.Code != "" {
			return envelope.Error.Code
		}
	}

	return "tool_error"
}

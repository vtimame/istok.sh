package mcpserver_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/mcpserver"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/storage"
	"github.com/vtimame/istok.sh/internal/storage/projectrepo"
)

type recordingSyncer struct {
	mu    sync.Mutex
	steps []string
}

func (r *recordingSyncer) add(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

func (r *recordingSyncer) SyncProject(context.Context, string) (bool, error) {
	r.add("sync")
	return true, nil
}

func (r *recordingSyncer) Nudge(string)          { r.add("nudge") }
func (r *recordingSyncer) Flush(context.Context) {}
func (r *recordingSyncer) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.steps
	r.steps = nil
	return s
}

func TestSyncFilterPullsBeforeTaskStartAndPushesAfterWrites(t *testing.T) {
	var db *sql.DB
	app := fx.New(fx.NopLogger,
		storage.Module(storage.Config{Path: filepath.Join(t.TempDir(), "istok.db")}),
		fx.Invoke(func(value *sql.DB) { db = value }))
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(ctx) })

	root := t.TempDir()
	projects := project.NewService(projectrepo.New(db))
	if _, err := projects.Init(ctx, root, "demo"); err != nil {
		t.Fatal(err)
	}

	syncer := &recordingSyncer{}
	handler := mcpserver.SyncFilter(syncer, projects, root)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		syncer.add("call")
		return &mcp.CallToolResult{}, nil
	})

	call := func(name string) []string {
		request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: name}}
		if _, err := handler(ctx, "tools/call", request); err != nil {
			t.Fatal(err)
		}
		return syncer.take()
	}

	for name, want := range map[string][]string{
		"task_start":    {"sync", "call", "nudge"},
		"task_progress": {"call", "nudge"},
		"task_show":     {"call"},
		"run_heartbeat": {"call"},
	} {
		if got := call(name); !slices.Equal(got, want) {
			t.Errorf("%s: steps %v, want %v", name, got, want)
		}
	}

	// Outside a project nothing syncs.
	outside := mcpserver.SyncFilter(syncer, projects, t.TempDir())(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})
	if _, err := outside(ctx, "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "task_start"}}); err != nil {
		t.Fatal(err)
	}
	if got := syncer.take(); len(got) != 0 {
		t.Errorf("outside a project: steps %v, want none", got)
	}
}

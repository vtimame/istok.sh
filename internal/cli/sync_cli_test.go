package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/server"
	"github.com/vtimame/istok.sh/internal/replica"
	"github.com/vtimame/istok.sh/internal/task"
)

// syncDevice is one machine: its own database, sync directory and folder.
type syncDevice struct {
	t        *testing.T
	root     string
	database string
	syncDir  string
}

func newSyncDevice(t *testing.T) *syncDevice {
	t.Helper()

	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	return &syncDevice{t: t, root: root, database: filepath.Join(base, "istok.db"), syncDir: filepath.Join(base, "sync")}
}

func (d *syncDevice) run(args ...string) executeResult {
	d.t.Helper()
	d.t.Setenv("ISTOK_SYNC_DIR", d.syncDir)

	return executeAt(d.t, d.root, d.database, args...)
}

func (d *syncDevice) must(args ...string) string {
	d.t.Helper()

	result := d.run(args...)
	if result.err != nil {
		d.t.Fatalf("%v: %v\n%s", args, result.err, result.output)
	}

	return result.output
}

type testSyncResult struct {
	Pulled    int `json:"pulled"`
	Pushed    int `json:"pushed"`
	Conflicts int `json:"conflicts"`
	Import    *struct {
		Conflicts []struct {
			Table string `json:"table"`
		} `json:"conflicts"`
	} `json:"import"`
}

// startHub runs a hub on a free local port until the test ends.
func startHub(t *testing.T) (*server.Hub, string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	hub, err := server.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- hub.Serve(ctx, server.ServeConfig{Listen: "127.0.0.1:0", Ready: func(address string) { ready <- address }})
	}()

	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("hub serve: %v", err)
		}
		_ = hub.Close()
	})

	select {
	case address := <-ready:
		return hub, address
	case err := <-done:
		t.Fatalf("hub serve: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("hub did not start")
	}

	return nil, ""
}

func invite(t *testing.T, hub *server.Hub, name, address string) string {
	t.Helper()

	invitation, err := hub.Invite(context.Background(), name, address, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	return invitation.Encode()
}

func TestSyncBetweenTwoDevicesThroughAHub(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	hub, address := startHub(t)
	arch, mac := newSyncDevice(t), newSyncDevice(t)

	// Arch creates the project and the key.
	initialized := arch.must("init", "--name", "demo", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized).Project.ID

	tasks, _ := newTaskRepositoryForTest(t, arch.database)
	actor := task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}
	if _, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "secret plan"}, actor); err != nil {
		t.Fatal(err)
	}

	joined := decodeJSONResult[struct {
		CreatedKey bool   `json:"created_key"`
		Recovery   string `json:"recovery"`
	}](t, arch.must("sync", "join", invite(t, hub, "arch", address), "--json"))
	if !joined.CreatedKey || !strings.HasPrefix(joined.Recovery, "istok-key-") {
		t.Fatalf("first join = %+v, want a new key", joined)
	}

	enabled := decodeJSONResult[[]testSyncResult](t, arch.must("sync", "enable", "--json"))
	if len(enabled) != 1 || enabled[0].Pushed == 0 {
		t.Fatalf("enable = %+v, want pushed rows", enabled)
	}

	// The hub cannot read what it stores.
	if contents := hubFiles(t, hub); bytes.Contains(contents, []byte("secret plan")) || bytes.Contains(contents, []byte("demo")) {
		t.Fatal("hub database contains plaintext project data")
	}

	// Mac needs the key, then fetches the project into its own folder.
	macCode := invite(t, hub, "mac", address)
	if result := mac.run("sync", "join", macCode); result.err == nil || !strings.Contains(result.err.Error(), "no sync key") {
		t.Fatalf("join without key: %v, want a missing key error", result.err)
	}
	mac.must("sync", "join", macCode, "--key", joined.Recovery)
	mac.must("sync", "enable", "--project", projectID)

	if list := mac.must("task", "list"); !strings.Contains(list, "secret plan") {
		t.Fatalf("mac task list = %q, want the synced task", list)
	}

	// Progress made on the mac reaches arch.
	mac.must("task", "progress", "1", "--body", "worked on mac")
	mac.must("sync")
	arch.must("sync")
	if shown := arch.must("task", "show", "1"); !strings.Contains(shown, "worked on mac") {
		t.Fatalf("arch task show = %q, want the mac progress", shown)
	}

	// A second round has nothing to move.
	idle := decodeJSONResult[[]testSyncResult](t, arch.must("sync", "--json"))
	if idle[0].Pulled != 0 || idle[0].Pushed != 0 {
		t.Fatalf("idle sync = %+v, want nothing moved", idle)
	}

	// Both devices change the task while apart: the history keeps both
	// events, the task row keeps one version and reports the conflict.
	mac.must("task", "progress", "1", "--body", "offline on mac")
	arch.must("task", "progress", "1", "--body", "offline on arch")
	mac.must("sync")
	conflicted := decodeJSONResult[[]testSyncResult](t, arch.must("sync", "--json"))
	if conflicted[0].Import == nil || len(conflicted[0].Import.Conflicts) == 0 {
		t.Fatalf("concurrent edits = %+v, want a reported conflict", conflicted)
	}
	mac.must("sync")
	for _, device := range []*syncDevice{arch, mac} {
		shown := device.must("task", "show", "1")
		if !strings.Contains(shown, "offline on mac") || !strings.Contains(shown, "offline on arch") {
			t.Fatalf("task show = %q, want both offline events", shown)
		}
	}

	// A revoked device is turned away.
	if _, err := hub.Store.Revoke(context.Background(), "mac"); err != nil {
		t.Fatal(err)
	}
	if result := mac.run("sync"); result.err == nil {
		t.Fatal("revoked device synced")
	}
}

func hubFiles(t *testing.T, hub *server.Hub) []byte {
	t.Helper()

	backup := filepath.Join(t.TempDir(), "copy.db")
	if err := hub.Store.Backup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestSessionStartHookAndNudgesSyncThroughTheHub(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	hub, address := startHub(t)
	arch, mac := newSyncDevice(t), newSyncDevice(t)

	initialized := arch.must("init", "--name", "demo", "--json")
	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, initialized).Project.ID

	tasks, _ := newTaskRepositoryForTest(t, arch.database)
	actor := task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}
	if _, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "evening work"}, actor); err != nil {
		t.Fatal(err)
	}

	key := decodeJSONResult[struct {
		Recovery string `json:"recovery"`
	}](t, arch.must("sync", "join", invite(t, hub, "arch", address), "--json")).Recovery
	arch.must("sync", "enable")
	mac.must("sync", "join", invite(t, hub, "mac", address), "--key", key)
	mac.must("sync", "enable", "--project", projectID)

	// Arch claims the task; a nudge pushes it without istok sync.
	arch.must("task", "claim", "1")
	nudgeAndFlush(t, arch, projectID)

	// A session starting on the mac sees the claimed task in its resume.
	t.Setenv("ISTOK_SYNC_DIR", mac.syncDir)
	hookInput := strings.NewReader(`{"cwd":"` + mac.root + `"}`)
	started := executeAtWithInput(t, mac.root, mac.database, hookInput, "hook", "claude", "session-start")
	if started.err != nil || !strings.Contains(started.output, "evening work") {
		t.Fatalf("session-start hook = %q, %v; want the task claimed on arch", started.output, started.err)
	}
}

// nudgeAndFlush does what an MCP server does after a write and on exit.
func nudgeAndFlush(t *testing.T, device *syncDevice, projectID string) {
	t.Helper()

	services, app := syncApplication(device.database)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Stop(ctx) }()

	syncer := replica.NewSyncer(device.syncDir, services.exchange, services.state, func(err error) { t.Errorf("sync: %v", err) })
	syncer.Nudge(projectID)
	syncer.Nudge(projectID)

	flushCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	syncer.Flush(flushCtx)
}

// Both devices change task 1 while apart: one edits it to a higher revision
// and keeps its dependencies, the other adds a blocker. After syncing, both
// devices hold the same dependencies.
func TestConcurrentDependencyChangesConverge(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	hub, address := startHub(t)
	arch, mac := newSyncDevice(t), newSyncDevice(t)

	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, arch.must("init", "--name", "demo", "--json")).Project.ID

	tasks, _ := newTaskRepositoryForTest(t, arch.database)
	actor := task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}
	var blockedID string
	for _, title := range []string{"blocked", "first blocker", "second blocker"} {
		created, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: title}, actor)
		if err != nil {
			t.Fatal(err)
		}
		if blockedID == "" {
			blockedID = created.ID
		}
	}

	key := decodeJSONResult[struct {
		Recovery string `json:"recovery"`
	}](t, arch.must("sync", "join", invite(t, hub, "arch", address), "--json")).Recovery
	arch.must("sync", "enable")
	mac.must("sync", "join", invite(t, hub, "mac", address), "--key", key)
	mac.must("sync", "enable", "--project", projectID)

	// Both devices hold task 1 blocked by task 2.
	arch.must("task", "dependency", "add", "1", "--blocker", "2")
	arch.must("sync")
	mac.must("sync")

	// The mac edits task 1 twice and leaves its dependencies alone; arch adds
	// a blocker, a lower revision with a different dependency group.
	macTasks, _ := newTaskRepositoryForTest(t, mac.database)
	blocked, err := macTasks.Get(context.Background(), blockedID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"edited once", "edited twice"} {
		blocked, err = macTasks.Update(context.Background(), blocked.ID, blocked.Revision, task.Patch{Title: &title}, actor)
		if err != nil {
			t.Fatal(err)
		}
	}
	arch.must("task", "dependency", "add", "1", "--blocker", "3")

	arch.must("sync")
	mac.must("sync")
	arch.must("sync")
	mac.must("sync")

	blockers := func(device *syncDevice) string {
		shown := device.must("task", "show", "1")
		_, rest, _ := strings.Cut(shown, "Blocked by:")
		line, _, _ := strings.Cut(rest, "\n")
		return strings.TrimSpace(stripANSI(line))
	}
	if left, right := blockers(arch), blockers(mac); left != right {
		t.Fatalf("dependencies diverged: arch %q, mac %q", left, right)
	}
}

// A device that lost its database restores the project
// from the hub, its own rows included.
func TestDeviceRestoresItsOwnRowsFromTheHub(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	hub, address := startHub(t)
	arch := newSyncDevice(t)

	projectID := decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, arch.must("init", "--name", "demo", "--json")).Project.ID

	tasks, _ := newTaskRepositoryForTest(t, arch.database)
	actor := task.ActorSnapshot{ID: "test", Kind: "test", Name: "Test"}
	if _, err := tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: "kept on the hub"}, actor); err != nil {
		t.Fatal(err)
	}

	arch.must("sync", "join", invite(t, hub, "arch", address))
	arch.must("sync", "enable")

	// Same device key and membership, fresh database and folder.
	restored := newSyncDevice(t)
	restored.syncDir = arch.syncDir
	restored.must("sync", "enable", "--project", projectID)

	if list := restored.must("task", "list"); !strings.Contains(list, "kept on the hub") {
		t.Fatalf("restored task list = %q, want the task", list)
	}
}

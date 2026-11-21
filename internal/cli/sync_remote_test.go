package cli

import (
	"os/exec"
	"strings"
	"testing"
)

// clone makes the device's folder a git repository with origin set to url.
func clone(t *testing.T, device *syncDevice, url string) {
	t.Helper()

	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", url}} {
		if output, err := exec.Command("git", append([]string{"-C", device.root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

// A fresh clone of the repository finds its hub project without an ID, and
// a separate local project of the same repository is not published unasked.
func TestSyncEnableFindsTheProjectOfARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("ISTOK_AGENT_ID", "")

	hub, address := startHub(t)
	arch, mac, other := newSyncDevice(t), newSyncDevice(t), newSyncDevice(t)
	clone(t, arch, "git@github.com:vtimame/demo.git")
	clone(t, mac, "https://github.com/vtimame/demo")
	clone(t, other, "https://github.com/vtimame/demo.git")

	arch.must("init", "--name", "demo")
	arch.must("task", "create", "--title", "made on arch")
	key := decodeJSONResult[struct {
		Recovery string `json:"recovery"`
	}](t, arch.must("sync", "join", invite(t, hub, "arch", address), "--json")).Recovery
	arch.must("sync", "enable")

	if listed := stripANSI(arch.must("sync", "projects")); !strings.Contains(listed, "github.com/vtimame/demo") {
		t.Fatalf("sync projects = %q, want the repository", listed)
	}

	mac.must("sync", "join", invite(t, hub, "mac", address), "--key", key)
	mac.must("sync", "enable")
	if list := mac.must("task", "list"); !strings.Contains(list, "made on arch") {
		t.Fatalf("mac task list = %q, want the arch task", list)
	}

	other.must("sync", "join", invite(t, hub, "other", address), "--key", key)
	other.must("init", "--name", "demo-copy")
	refused := other.run("sync", "enable")
	if refused.err == nil || !strings.Contains(refused.err.Error(), "--new") {
		t.Fatalf("sync enable of a duplicate = %q, %v; want a refusal suggesting --new", refused.output, refused.err)
	}

	other.must("sync", "enable", "--new")
	if listed := stripANSI(other.must("sync", "projects")); !strings.Contains(listed, "demo-copy") {
		t.Fatalf("sync projects = %q, want demo-copy published", listed)
	}
}

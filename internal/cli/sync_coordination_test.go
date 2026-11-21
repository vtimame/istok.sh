package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vtimame/istok.sh/internal/replica"
)

// joinedPair returns two devices syncing one project through a hub.
func joinedPair(t *testing.T) (arch, mac *syncDevice, projectID string) {
	t.Helper()

	hub, address := startHub(t)
	arch, mac = newSyncDevice(t), newSyncDevice(t)

	projectID = decodeJSONResult[struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}](t, arch.must("init", "--name", "demo", "--json")).Project.ID

	key := decodeJSONResult[struct {
		Recovery string `json:"recovery"`
	}](t, arch.must("sync", "join", invite(t, hub, "arch", address), "--json")).Recovery
	arch.must("sync", "enable")
	mac.must("sync", "join", invite(t, hub, "mac", address), "--key", key)
	mac.must("sync", "enable", "--project", projectID)

	return arch, mac, projectID
}

func titleOf(t *testing.T, device *syncDevice, number string) string {
	t.Helper()

	// The title follows the "project · #N" line and a blank line.
	lines := strings.Split(stripANSI(device.must("task", "show", number)), "\n")
	if len(lines) < 3 {
		t.Fatalf("task show %s = %q", number, lines)
	}

	return lines[2]
}

func TestHubHandsOutTaskNumbers(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	arch, mac, _ := joinedPair(t)

	// Neither device syncs in between; the hub keeps the numbers apart.
	arch.must("task", "create", "--title", "made on arch")
	mac.must("task", "create", "--title", "made on mac")

	arch.must("sync")
	mac.must("sync")
	arch.must("sync")

	for _, device := range []*syncDevice{arch, mac} {
		if first := titleOf(t, device, "1"); !strings.Contains(first, "made on arch") {
			t.Fatalf("task 1 = %q, want the arch task", first)
		}
		if second := titleOf(t, device, "2"); !strings.Contains(second, "made on mac") {
			t.Fatalf("task 2 = %q, want the mac task", second)
		}
		for _, number := range []string{"1", "2"} {
			if shown := device.must("task", "show", number); strings.Contains(shown, "Renumbered") {
				t.Fatalf("task %s was renumbered:\n%s", number, shown)
			}
		}
	}

	// A task made while the hub is out of reach takes a local number and
	// moves to a free one on the next sync.
	offline := t.TempDir()
	t.Setenv("ISTOK_SYNC_DIR", offline)
	if result := executeAt(t, mac.root, mac.database, "task", "create", "--title", "made offline"); result.err != nil {
		t.Fatal(result.err)
	}
	arch.must("task", "create", "--title", "made on arch again")

	synced := decodeJSONResult[[]struct {
		Renumbered []struct {
			From int64 `json:"from"`
			To   int64 `json:"to"`
		} `json:"renumbered"`
	}](t, mac.must("sync", "--json"))
	if len(synced[0].Renumbered) != 1 || synced[0].Renumbered[0].From != 3 || synced[0].Renumbered[0].To != 4 {
		t.Fatalf("renumbered = %+v, want the offline task moved from 3 to 4", synced[0].Renumbered)
	}

	arch.must("sync")
	mac.must("sync")
	for _, device := range []*syncDevice{arch, mac} {
		if third := titleOf(t, device, "3"); !strings.Contains(third, "made on arch again") {
			t.Fatalf("task 3 = %q, want the arch task", third)
		}
		if fourth := titleOf(t, device, "4"); !strings.Contains(fourth, "made offline") {
			t.Fatalf("task 4 = %q, want the offline task", fourth)
		}
	}
}

func TestHubRefusesATaskAnotherDeviceWorksOn(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	arch, mac, _ := joinedPair(t)

	arch.must("task", "create", "--title", "shared work")
	arch.must("sync")
	mac.must("sync")

	// Arch claims and does not sync; the mac still asks the hub first.
	claimed := decodeJSONResultAtVersion[struct {
		ID       string `json:"id"`
		LeaseID  string `json:"lease_id"`
		Revision int64  `json:"revision"`
	}](t, arch.must("task", "claim", "1", "--json"), "2")
	refused := mac.run("task", "claim", "1")
	if refused.err == nil || !strings.Contains(refused.output+refused.err.Error(), "device arch") {
		t.Fatalf("claim on the mac = %q, %v; want a refusal naming arch", refused.output, refused.err)
	}

	// Once arch ends its run and syncs, the lease is released.
	arch.must("run", "finish", claimed.ID, "--lease", claimed.LeaseID, "--expected-revision", strconv.FormatInt(claimed.Revision, 10),
		"--status", "cancelled", "--summary", "handing over")
	arch.must("sync")
	mac.must("sync")
	mac.must("task", "claim", "1")
}

// Reading tasks syncs first, so a device sees what another one did without
// istok sync; a hub out of reach is skipped quickly.
func TestTaskListShowsWhatAnotherDeviceDid(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	arch, mac, _ := joinedPair(t)

	previous := replica.RefreshAge
	replica.RefreshAge = 0
	t.Cleanup(func() { replica.RefreshAge = previous })

	arch.must("task", "create", "--title", "made on arch")
	arch.must("sync")

	if list := mac.must("task", "list"); !strings.Contains(list, "made on arch") {
		t.Fatalf("mac task list = %q, want the arch task without istok sync", list)
	}

	// The hub goes away: the read still answers and marks the hub as out of
	// reach, so the next read does not wait for it.
	config, err := replica.LoadConfig(mac.syncDir)
	if err != nil {
		t.Fatal(err)
	}
	config.HubAddress = "127.0.0.1:1"
	if err := replica.SaveConfig(mac.syncDir, config); err != nil {
		t.Fatal(err)
	}

	mac.must("task", "list")
	if _, err := os.Stat(filepath.Join(mac.syncDir, "hub-unreachable")); err != nil {
		t.Fatalf("unreachable hub not remembered: %v", err)
	}
}

// The task list shows who works on a task and on which device, on the
// device that claimed it and on the other one after a sync.
func TestTaskListShowsWhoWorksOnATask(t *testing.T) {
	t.Setenv("ISTOK_AGENT_ID", "")
	arch, mac, _ := joinedPair(t)

	arch.must("task", "create", "--title", "shared work")
	arch.must("task", "create", "--title", "nobody's work")
	arch.must("task", "claim", "1")
	arch.must("sync")
	mac.must("sync")

	for _, device := range []*syncDevice{arch, mac} {
		listed := decodeJSONResult[struct {
			Tasks []struct {
				Number       int64  `json:"number"`
				Worker       string `json:"worker"`
				WorkerDevice string `json:"worker_device"`
			} `json:"tasks"`
		}](t, device.must("task", "list", "--json"))
		workers := map[int64]string{}
		for _, value := range listed.Tasks {
			workers[value.Number] = value.Worker + "@" + value.WorkerDevice
		}
		if workers[1] != "CLI@arch" || workers[2] != "@" {
			t.Fatalf("workers = %v, want task 1 held by the CLI on arch and task 2 free", workers)
		}

		if table := stripANSI(device.must("task", "list")); !strings.Contains(table, "WORKER") || !strings.Contains(table, "CLI · arch") {
			t.Fatalf("task list = %q, want a WORKER column with CLI · arch", table)
		}
	}
}

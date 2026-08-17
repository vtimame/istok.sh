//go:build hardening

package hardening

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLargeRepositoryLifecycle(t *testing.T) {
	e := newEnvironment(t)
	writeCorpus(t, e.root, corpusSize(t))

	started := time.Now()
	e.runJSON(t, "init", "--json")
	initial := status(t, e)
	project := projectID(t, e)
	epoch := stringValue(t, initial, "epoch_id")
	revision := numberValue(t, initial, "revision")
	if epoch == "" || revision < 1 {
		t.Fatalf("initial status = %#v", initial)
	}
	if strings.HasPrefix(generationDir(e, project, epoch), e.root+string(filepath.Separator)) {
		t.Fatalf("sidecar is inside project: %s", generationDir(e, project, epoch))
	}

	unchanged := e.runBoundedJSON(t, 64*1024, "search", "needle-0000", "--limit", "3", "--json")
	if len(array(t, object(t, unchanged, "result"), "results")) == 0 {
		t.Fatalf("unchanged search = %#v", unchanged)
	}
	if got := status(t, e); stringValue(t, got, "epoch_id") != epoch || numberValue(t, got, "revision") != revision {
		t.Fatalf("unchanged search changed index: %#v", got)
	}

	if err := os.WriteFile(filepath.Join(e.root, "added.go"), []byte("package fixture\nfunc NewlyIndexedHardeningSymbol() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := e.runBoundedJSON(t, 64*1024, "search", "NewlyIndexedHardeningSymbol", "--limit", "3", "--json")
	if len(array(t, object(t, result, "result"), "results")) == 0 {
		t.Fatalf("incremental search = %#v", result)
	}
	updated := status(t, e)
	if stringValue(t, updated, "epoch_id") != epoch || numberValue(t, updated, "revision") <= revision {
		t.Fatalf("incremental status = %#v, previous=%#v", updated, initial)
	}

	size := directorySize(t, filepath.Join(e.indexRoot, project))
	t.Logf("large lifecycle: files=%d init=%s sidecar=%d bytes epoch=%s revision=%d", corpusSize(t), time.Since(started).Round(time.Millisecond), size, epoch, numberValue(t, updated, "revision"))
}

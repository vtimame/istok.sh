//go:build hardening

package hardening

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInterruptedRebuildAndCorruptionRecovery(t *testing.T) {
	e := newEnvironment(t)
	writeCorpus(t, e.root, corpusSize(t))
	e.runJSON(t, "init", "--json")
	project, prior := projectID(t, e), currentEpoch(t, e)

	ctx, cancel := context.WithTimeout(context.Background(), hardeningTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, os.Getenv("ISTOK_HARDENING_BINARY"), "index", "rebuild", "--json")
	command.Dir, command.Env = e.root, e.env
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start rebuild: %v", err)
	}
	waited := false
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			<-wait
		}
	}()
	var abandoned string
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-wait:
			waited = true
			t.Fatalf("rebuild exited before interruption point: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		default:
		}

		current := currentEpochOnDisk(t, e, project)
		for _, epoch := range generationNames(t, e, project) {
			if epoch != prior && current == prior {
				abandoned = epoch
				break
			}
		}
		if abandoned != "" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if abandoned == "" {
		t.Fatalf("rebuild did not create an unpublished generation before %s", deadline)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill rebuild: %v", err)
	}
	if err := <-wait; err == nil {
		t.Fatal("interrupted rebuild exited successfully")
	}
	waited = true
	if got := currentEpoch(t, e); got != prior {
		t.Fatalf("CURRENT moved after interrupted rebuild: got %s, want %s", got, prior)
	}
	e.runJSON(t, "search", "needle-0000", "--json")
	if names := generationNames(t, e, project); contains(names, abandoned) {
		t.Fatalf("abandoned generation %s was not cleaned: %v", abandoned, names)
	}

	active := currentEpoch(t, e)
	if err := os.Remove(filepath.Join(generationDir(e, project, active), "graph.db")); err != nil {
		t.Fatalf("remove active graph.db: %v", err)
	}
	e.runJSON(t, "search", "needle-0000", "--json")
	afterGraph := currentEpoch(t, e)
	if afterGraph == active {
		t.Fatalf("graph corruption did not rebuild epoch %s", active)
	}

	if err := os.WriteFile(filepath.Join(e.indexRoot, project, "CURRENT"), []byte("broken-current\n"), 0o600); err != nil {
		t.Fatalf("corrupt CURRENT: %v", err)
	}
	e.runJSON(t, "search", "needle-0000", "--json")
	if afterCurrent := currentEpoch(t, e); afterCurrent == afterGraph {
		t.Fatalf("CURRENT corruption did not rebuild epoch %s", afterGraph)
	}
}

func TestParserDegradationAndProjectLifecycle(t *testing.T) {
	e := newEnvironment(t)
	if err := os.WriteFile(filepath.Join(e.root, "broken.go"), []byte("package fixture\nfunc Broken( { lexicalDegradedToken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, "broken.ts"), []byte("export function broken( { lexicalTypeScriptToken"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.runJSON(t, "init", "--json")
	search := e.runJSON(t, "search", "lexicalDegradedToken", "--json")
	if len(array(t, object(t, search, "result"), "results")) == 0 {
		t.Fatalf("lexical search unavailable: %#v", search)
	}
	if state := stringValue(t, object(t, search, "result", "status"), "state"); state != "degraded" {
		t.Fatalf("parser status = %q, want degraded", state)
	}

	project := projectID(t, e)
	newRoot := filepath.Join(t.TempDir(), "rebound")
	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newRoot, "new.go"), []byte("package rebound\nfunc ReboundHardeningSymbol() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.runJSON(t, "project", "rebind", project, newRoot, "--json")
	e.root = newRoot
	if len(array(t, object(t, e.runJSON(t, "search", "ReboundHardeningSymbol", "--json"), "result"), "results")) == 0 {
		t.Fatal("rebind did not rebuild index")
	}
	e.runJSON(t, "project", "delete", "--yes", "--json")
	e.runJSON(t, "project", "restore", project, newRoot, "--json")
	if restored := projectID(t, e); restored != project {
		t.Fatalf("restored project ID = %q, want %q", restored, project)
	}
	e.runJSON(t, "index", "rebuild", "--json")
	if len(array(t, object(t, e.runJSON(t, "search", "ReboundHardeningSymbol", "--json"), "result"), "results")) == 0 {
		t.Fatal("restored project cannot index")
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

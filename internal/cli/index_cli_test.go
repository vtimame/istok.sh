package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexCommandsExposeStatusSearchAndGraphForCurrentProject(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	writeIndexFixture(t, root)

	if result := executeAt(t, root, database, "init", "--json"); result.err != nil {
		t.Fatalf("init: %v", result.err)
	}

	status := executeAt(t, root, database, "index", "status", "--json")
	if status.err != nil {
		t.Fatalf("index status: %v", status.err)
	}
	statusValue := decodeJSONResult[struct {
		Status struct {
			State    string `json:"state"`
			EpochID  string `json:"epoch_id"`
			Revision int64  `json:"revision"`
		} `json:"status"`
	}](t, status.output)
	if statusValue.Status.State != "ready" || statusValue.Status.EpochID == "" || statusValue.Status.Revision == 0 {
		t.Fatalf("index status = %+v", statusValue.Status)
	}

	search := executeAt(t, root, database, "search", "CreateInvoice", "--limit", "3", "--json")
	if search.err != nil {
		t.Fatalf("search: %v", search.err)
	}
	searchValue := decodeJSONResult[struct {
		ContractVersion string `json:"contract_version"`
		Status          struct {
			State string `json:"state"`
		} `json:"status"`
		Results []struct {
			ContractVersion string `json:"contract_version"`
			Path            string `json:"path"`
			Snippet         string `json:"snippet"`
		} `json:"results"`
	}](t, search.output)
	if searchValue.ContractVersion != "istok.retrieval.v1" || searchValue.Status.State != "ready" || len(searchValue.Results) == 0 {
		t.Fatalf("search result = %+v", searchValue)
	}
	if searchValue.Results[0].ContractVersion == "" || searchValue.Results[0].Path != "invoice.go" || searchValue.Results[0].Snippet == "" {
		t.Fatalf("search result item = %+v", searchValue.Results[0])
	}

	symbols := executeAt(t, root, database, "graph", "symbol", "CreateInvoice", "--json")
	if symbols.err != nil {
		t.Fatalf("graph symbol: %v", symbols.err)
	}
	symbolValue := decodeJSONResult[struct {
		ContractVersion string `json:"contract_version"`
		Symbols         []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"symbols"`
	}](t, symbols.output)
	if symbolValue.ContractVersion != "istok.graph.v1" || len(symbolValue.Symbols) == 0 || symbolValue.Symbols[0].Name != "CreateInvoice" {
		t.Fatalf("graph symbols = %+v", symbolValue)
	}

	neighbors := executeAt(t, root, database, "graph", "neighbors", "CreateInvoice", "--kind", "calls", "--json")
	if neighbors.err != nil {
		t.Fatalf("graph neighbors: %v", neighbors.err)
	}
	neighborValue := decodeJSONResult[struct {
		Neighbors []struct {
			Kind   string `json:"kind"`
			Source struct {
				Name string `json:"name"`
			} `json:"source"`
			Target struct {
				Name string `json:"name"`
			} `json:"target"`
		} `json:"neighbors"`
	}](t, neighbors.output)
	if len(neighborValue.Neighbors) == 0 || neighborValue.Neighbors[0].Kind != "calls" || neighborValue.Neighbors[0].Source.Name != "CreateInvoice" || neighborValue.Neighbors[0].Target.Name != "persistInvoice" {
		t.Fatalf("graph neighbors = %+v", neighborValue)
	}

	paths := executeAt(t, root, database, "graph", "path", "CreateInvoice", "persistInvoice", "--json")
	if paths.err != nil {
		t.Fatalf("graph path: %v", paths.err)
	}
	pathValue := decodeJSONResult[struct {
		Paths []struct {
			Nodes []struct {
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"paths"`
	}](t, paths.output)
	if len(pathValue.Paths) == 0 || len(pathValue.Paths[0].Nodes) < 2 {
		t.Fatalf("graph paths = %+v", pathValue)
	}
}

func TestIndexRebuildChangesEpochAndSearchRefreshesChangedFiles(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	writeIndexFixture(t, root)
	if result := executeAt(t, root, database, "init", "--json"); result.err != nil {
		t.Fatalf("init: %v", result.err)
	}

	before := indexEpoch(t, executeAt(t, root, database, "index", "status", "--json").output)
	rebuilt := executeAt(t, root, database, "index", "rebuild", "--json")
	if rebuilt.err != nil {
		t.Fatalf("index rebuild: %v", rebuilt.err)
	}
	after := indexEpoch(t, rebuilt.output)
	if before == after {
		t.Fatalf("rebuild did not change epoch: %q", before)
	}

	beforeRevision := indexRevision(t, rebuilt.output)
	if err := os.WriteFile(filepath.Join(root, "added.go"), []byte("package fixture\n\nfunc AddedAfterInit() {}\n"), 0o600); err != nil {
		t.Fatalf("write added source: %v", err)
	}
	inspected := executeAt(t, root, database, "index", "status", "--json")
	if inspected.err != nil {
		t.Fatalf("index status after source change: %v", inspected.err)
	}
	if revision := indexRevision(t, inspected.output); revision != beforeRevision {
		t.Fatalf("index status refreshed revision: got %d, want %d", revision, beforeRevision)
	}

	search := executeAt(t, root, database, "search", "AddedAfterInit", "--json")
	if search.err != nil {
		t.Fatalf("search after update: %v", search.err)
	}
	value := decodeJSONResult[struct {
		Status struct {
			Revision int64 `json:"revision"`
		} `json:"status"`
		Results []struct {
			Path string `json:"path"`
		} `json:"results"`
	}](t, search.output)
	if len(value.Results) == 0 || value.Results[0].Path != "added.go" {
		t.Fatalf("refreshed search = %+v", value)
	}
	if value.Status.Revision <= beforeRevision {
		t.Fatalf("search did not refresh revision: got %d, previous %d", value.Status.Revision, beforeRevision)
	}
}

func TestIndexCLIUsesPWDAndDoesNotWriteANSIToBuffer(t *testing.T) {
	database := filepath.Join(t.TempDir(), "istok.db")
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "first.go"), []byte("package first\nfunc FirstScope() {}\n"), 0o600); err != nil {
		t.Fatalf("write first source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, "second.go"), []byte("package second\nfunc SecondScope() {}\n"), 0o600); err != nil {
		t.Fatalf("write second source: %v", err)
	}
	for _, root := range []string{first, second} {
		if result := executeAt(t, root, database, "init", "--json"); result.err != nil {
			t.Fatalf("init %q: %v", root, result.err)
		}
	}

	firstResult := executeAt(t, first, database, "search", "FirstScope", "--json")
	if firstResult.err != nil || !strings.Contains(firstResult.output, "first.go") || strings.Contains(firstResult.output, "second.go") {
		t.Fatalf("first project search = (%q, %v)", firstResult.output, firstResult.err)
	}

	var output, errorOutput bytes.Buffer
	err := ExecuteAt(context.Background(), []string{"index", "status", "--database", database}, strings.NewReader(""), &output, &errorOutput, first)
	if err != nil {
		t.Fatalf("human index status: %v", err)
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("buffer output contains ANSI: %q", output.String())
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create output pipe: %v", err)
	}
	err = ExecuteAt(context.Background(), []string{"index", "status", "--database", database}, strings.NewReader(""), writer, io.Discard, first)
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close output pipe: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("pipe index status: %v", err)
	}
	pipeOutput, readErr := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close input pipe: %v", closeErr)
	}
	if readErr != nil {
		t.Fatalf("read output pipe: %v", readErr)
	}
	if strings.Contains(string(pipeOutput), "\x1b[") {
		t.Fatalf("pipe output contains ANSI: %q", pipeOutput)
	}
}

func TestIndexCompletionIncludesCommands(t *testing.T) {
	for line, required := range map[string][]string{
		"istok ":       {"index", "search", "graph"},
		"istok index ": {"status", "rebuild"},
		"istok graph ": {"symbol", "neighbors", "path"},
	} {
		options := completionSuggestionLines(t, line)
		for _, command := range required {
			if !containsCompletion(options, command) {
				t.Errorf("completion at %q does not include %q: %v", line, command, options)
			}
		}
	}
}

func TestIndexParseErrorsDoNotCreateDatabase(t *testing.T) {
	dataDir := t.TempDir()
	setXDGDataHome(t, dataDir)
	for _, args := range [][]string{{"search"}, {"graph", "symbol"}, {"graph", "path", "only-one"}} {
		var output, errorOutput bytes.Buffer
		if err := ExecuteAt(context.Background(), args, strings.NewReader(""), &output, &errorOutput, t.TempDir()); err == nil {
			t.Fatalf("expected parse error for %q", args)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "istok", "istok.db")); !os.IsNotExist(err) {
		t.Fatalf("database was unexpectedly created, stat error = %v", err)
	}
}

func writeIndexFixture(t *testing.T, root string) {
	t.Helper()
	content := "package fixture\n\nfunc CreateInvoice() {\n\tpersistInvoice()\n}\n\nfunc persistInvoice() {}\n"
	if err := os.WriteFile(filepath.Join(root, "invoice.go"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func indexEpoch(t *testing.T, output string) string {
	t.Helper()
	value := decodeJSONResult[struct {
		Status struct {
			EpochID string `json:"epoch_id"`
		} `json:"status"`
	}](t, output)
	return value.Status.EpochID
}

func indexRevision(t *testing.T, output string) int64 {
	t.Helper()
	value := decodeJSONResult[struct {
		Status struct {
			Revision int64 `json:"revision"`
		} `json:"status"`
	}](t, output)

	return value.Status.Revision
}

func containsCompletion(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func TestIndexJSONIsValid(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	writeIndexFixture(t, root)
	if result := executeAt(t, root, database, "init", "--json"); result.err != nil {
		t.Fatalf("init: %v", result.err)
	}
	result := executeAt(t, root, database, "index", "status", "--json")
	if result.err != nil || !json.Valid([]byte(result.output)) {
		t.Fatalf("index JSON = (%q, %v)", result.output, result.err)
	}
}

func TestIndexSearchReturnsStableJSONIndexFailure(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	writeIndexFixture(t, root)
	if result := executeAt(t, root, database, "init", "--json"); result.err != nil {
		t.Fatalf("init: %v", result.err)
	}

	brokenIndexRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(brokenIndexRoot, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write blocked index root: %v", err)
	}
	t.Setenv("ISTOK_INDEX_ROOT", brokenIndexRoot)

	var output, errorOutput bytes.Buffer
	err := ExecuteAt(
		context.Background(),
		[]string{"search", "CreateInvoice", "--database", database, "--json"},
		strings.NewReader(""),
		&output,
		&errorOutput,
		root,
	)
	if err == nil {
		t.Fatal("search unexpectedly succeeded with blocked index root")
	}
	assertVersionedBusinessError(t, err, "index_failed")
}

func TestIndexJSONUsesVersionedEmptyCollections(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "istok.db")
	writeIndexFixture(t, root)
	if result := executeAt(t, root, database, "init", "--json"); result.err != nil {
		t.Fatalf("init: %v", result.err)
	}

	for _, test := range []struct {
		args []string
		key  string
	}{
		{args: []string{"search", "DefinitelyMissing", "--json"}, key: "results"},
		{args: []string{"graph", "symbol", "DefinitelyMissing", "--json"}, key: "symbols"},
		{args: []string{"graph", "neighbors", "persistInvoice", "--kind", "calls", "--json"}, key: "neighbors"},
		{args: []string{"graph", "path", "persistInvoice", "CreateInvoice", "--json"}, key: "paths"},
	} {
		result := executeAt(t, root, database, test.args...)
		if result.err != nil {
			t.Fatalf("%v: %v", test.args, result.err)
		}
		if !strings.Contains(result.output, fmt.Sprintf("%q:[]", test.key)) {
			t.Errorf("%v returned nullable %s: %s", test.args, test.key, result.output)
		}
	}
}

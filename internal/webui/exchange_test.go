package webui_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vtimame/istok.sh/internal/exchange"
	"github.com/vtimame/istok.sh/internal/project"
)

func TestExportRequiresASelection(t *testing.T) {
	f := newFixture(t)

	requireError(t, f.do(t, http.MethodGet, "/api/v1/export", "", nil), http.StatusBadRequest, "invalid_argument")
}

func TestExportDownloadsABundleAndImportMergesIt(t *testing.T) {
	source := newFixture(t)
	acme := source.newProject(t, "acme")
	source.newTask(t, acme.ID, "Add pagination")

	exported := source.do(t, http.MethodGet, "/api/v1/export?project="+acme.ID, "", nil)
	if exported.status != http.StatusOK {
		t.Fatalf("export = %d %s", exported.status, exported.body)
	}
	if disposition := exported.header.Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment;") {
		t.Fatalf("Content-Disposition = %q, want a download", disposition)
	}

	target := newFixture(t)

	var preview exchange.Report
	requireResult(t, target.doJSON(t, http.MethodPost, "/api/v1/import?dry_run=1", string(exported.body)), &preview)
	if !preview.DryRun || preview.Tables["tasks"].Created != 1 {
		t.Fatalf("dry run report = %+v", preview)
	}
	if listed, err := target.projects.List(context.Background(), false); err != nil || len(listed) != 0 {
		t.Fatalf("projects after dry run = %+v, %v", listed, err)
	}

	var report exchange.Report
	requireResult(t, target.doJSON(t, http.MethodPost, "/api/v1/import", string(exported.body)), &report)
	if len(report.Projects) != 1 || !report.Projects[0].Created || report.Projects[0].Bound {
		t.Fatalf("import report = %+v", report.Projects)
	}

	var listed []project.Project
	requireResult(t, target.do(t, http.MethodGet, "/api/v1/projects", "", nil), &listed)
	if len(listed) != 1 || listed[0].ID != acme.ID || listed[0].Root != nil {
		t.Fatalf("projects = %+v, want the imported project without a folder", listed)
	}
}

func TestImportRejectsWrongContentAndBrokenBundles(t *testing.T) {
	f := newFixture(t)

	unsupported := f.do(t, http.MethodPost, "/api/v1/import", "{}", map[string]string{"Content-Type": "text/plain"})
	requireError(t, unsupported, http.StatusUnsupportedMediaType, "unsupported_media_type")
	requireError(t, f.doJSON(t, http.MethodPost, "/api/v1/import", `{"format":"something-else"}`), http.StatusBadRequest, "invalid_argument")
}

func TestBindAttachesAFolderToAnImportedProject(t *testing.T) {
	source := newFixture(t)
	acme := source.newProject(t, "acme")
	exported := source.do(t, http.MethodGet, "/api/v1/export?project="+acme.ID, "", nil)

	target := newFixture(t)
	var report exchange.Report
	requireResult(t, target.doJSON(t, http.MethodPost, "/api/v1/import", string(exported.body)), &report)

	bindPath := "/api/v1/projects/" + acme.ID + "/bind"
	requireError(t, target.doJSON(t, http.MethodPost, bindPath, `{"path":"relative/path","expected_revision":1}`), http.StatusBadRequest, "invalid_argument")
	requireError(t, target.doJSON(t, http.MethodPost, bindPath, `{"path":"/tmp"}`), http.StatusBadRequest, "invalid_argument")

	folder := filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(folder)
	if err != nil {
		t.Fatal(err)
	}

	imported, err := target.projects.Resolve(context.Background(), acme.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	var bound project.Project
	requireResult(t, target.doJSON(t, http.MethodPost, bindPath, `{"path":"`+resolved+`","expected_revision":`+itoa(imported.Revision)+`}`), &bound)
	if bound.Root == nil || bound.Root.CanonicalPath != resolved {
		t.Fatalf("bound project = %+v, want root %q", bound, resolved)
	}
}

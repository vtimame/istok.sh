package webui

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	exchangeapp "github.com/vtimame/istok.sh/internal/application/exchange"
)

// uiExchangeActor attributes history written by imports from the web UI.
var uiExchangeActor = exchangeapp.Actor{ID: uiActor.ID, Kind: uiActor.Kind, Name: uiActor.Name}

func (a api) registerExchange(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/export", a.exportProjects) // ?project=ID (repeatable) or ?all=1
	mux.HandleFunc("POST /api/v1/import", a.importBundle)  // ?dry_run=1
	mux.HandleFunc("POST /api/v1/projects/{project}/bind", a.bindProject)
}

// exportProjects downloads a bundle. The browser saves it as a file; other
// sites cannot read the response because the server never allows CORS.
func (a api) exportProjects(w http.ResponseWriter, r *http.Request) {
	selection := exchangeapp.Selection{All: r.URL.Query().Get("all") == "1", Selectors: r.URL.Query()["project"]}
	if !selection.All && len(selection.Selectors) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "choose projects with ?project=ID or export everything with ?all=1")
		return
	}

	bundle, err := a.services.Exchange.Export(r.Context(), selection)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	name := fmt.Sprintf("istok-export-%s.json", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	_ = exchangeapp.Encode(w, bundle)
}

// importBundle merges an uploaded bundle. The body is the bundle itself, sent
// as application/json like every other mutation.
func (a api) importBundle(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "request body must be application/json")
		return
	}

	bundle, err := exchangeapp.Decode(http.MaxBytesReader(w, r.Body, exchangeapp.MaxBundleBytes))
	if err != nil {
		writeDomainError(w, err)
		return
	}

	dryRun := r.URL.Query().Get("dry_run") == "1"
	report, err := a.services.Exchange.Import(r.Context(), bundle, uiExchangeActor, dryRun)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	if !dryRun {
		a.broker.publish()
	}
	writeResult(w, report)
}

type bindRequest struct {
	Path             string `json:"path"`
	ExpectedRevision int64  `json:"expected_revision"`
}

// bindProject attaches a folder on this device to a project, typically one
// that arrived by import. The path must be absolute or start with ~/, because
// the server's working directory means nothing to the person typing it.
func (a api) bindProject(w http.ResponseWriter, r *http.Request) {
	var body bindRequest
	if !decodeBody(w, r, &body) {
		return
	}
	if body.ExpectedRevision < 1 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "expected_revision is required")
		return
	}

	path, ok := expandHome(strings.TrimSpace(body.Path))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_argument", "enter an absolute folder path, for example ~/src/acme-api")
		return
	}

	bound, err := a.services.Projects.Rebind(r.Context(), r.PathValue("project"), path, &body.ExpectedRevision)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	a.broker.publish()
	writeResult(w, bound)
}

func expandHome(path string) (string, bool) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}

	return path, filepath.IsAbs(path)
}

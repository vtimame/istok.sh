package webui

import (
	"encoding/json"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/vtimame/istok.sh/internal/run"
)

const maxBodyBytes = 64 << 10

// uiActor attributes changes made through the web UI in run and task history.
var uiActor = run.ActorSnapshot{ID: "web-ui", Kind: "user", Name: "Web UI"}

func (a api) registerMutations(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/runs/{run}/abandon", a.abandonRun)
	mux.HandleFunc("DELETE /api/v1/projects/{project}", a.deleteProject)
}

// decodeBody requires a JSON content type: a cross-site form cannot send it
// without a CORS preflight, which this server never approves.
func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "request body must be application/json")
		return false
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON body: "+err.Error())
		return false
	}

	return true
}

type abandonRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}

// abandonRun closes a stale run: one that is still active but whose agent
// stopped heartbeating. Live runs are refused so the UI cannot pull work out
// from under a working agent; the revision check covers a heartbeat racing
// with this request.
func (a api) abandonRun(w http.ResponseWriter, r *http.Request) {
	var body abandonRequest
	if !decodeBody(w, r, &body) {
		return
	}

	runID := r.PathValue("run")
	current, err := a.services.Runs.GetRun(r.Context(), runID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if current.Status == run.StatusActive && current.ExpiresAt.After(time.Now()) {
		writeError(w, http.StatusConflict, "run_not_stale", "the run is still heartbeating; only stale runs can be abandoned from the UI")
		return
	}

	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "Abandoned from the web UI: the agent stopped sending heartbeats."
	}

	abandoned, err := a.services.Runs.Abandon(r.Context(), run.AbandonInput{
		RunID:            runID,
		ExpectedRevision: body.ExpectedRevision,
		Reason:           reason,
	}, uiActor)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	a.broker.publish()
	writeResult(w, abandoned)
}

type deleteProjectRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
}

// deleteProject soft-deletes a project; `istok project restore` brings it back.
func (a api) deleteProject(w http.ResponseWriter, r *http.Request) {
	var body deleteProjectRequest
	if !decodeBody(w, r, &body) {
		return
	}
	if body.ExpectedRevision < 1 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "expected_revision is required")
		return
	}

	deleted, err := a.services.Projects.Delete(r.Context(), r.PathValue("project"), &body.ExpectedRevision)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	a.broker.publish()
	writeResult(w, deleted)
}

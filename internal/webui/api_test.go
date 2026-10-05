package webui_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	contextpackapp "github.com/vtimame/istok.sh/internal/application/contextpack"
	exchangeapp "github.com/vtimame/istok.sh/internal/application/exchange"
	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/storage/exchangerepo"
	"github.com/vtimame/istok.sh/internal/storage/runrepo"
	"github.com/vtimame/istok.sh/internal/storage/taskrepo"
	"github.com/vtimame/istok.sh/internal/storage/uireadrepo"
	"github.com/vtimame/istok.sh/internal/task"
	"github.com/vtimame/istok.sh/internal/webui"
)

const testPort = "7700"

var (
	testTaskActor = task.ActorSnapshot{ID: "local-user", Kind: "user", Name: "Local User"}
	testRunActor  = run.ActorSnapshot{ID: "local-agent", Kind: "agent", Name: "Local Agent"}
)

// fixture serves the UI handler over the same kernel services as
// bootstrap.UIApp. staleRuns claims through a repository whose clock is an
// hour behind, so its runs start with an already expired lease.
type fixture struct {
	handler   http.Handler
	projects  *project.Service
	tasks     *taskapp.Service
	runs      *runapp.Service
	staleRuns *runapp.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	t.Setenv("ISTOK_INDEX_ROOT", t.TempDir())
	database := filepath.Join(t.TempDir(), "istok.db")

	f := &fixture{}
	var services webui.Services
	var db *sql.DB
	var taskRepository *taskrepo.Repository
	var contextBuilder *contextpackapp.Service

	app := fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		bootstrap.TaskOptions(database),
		fx.Provide(uireadrepo.New),
		fx.Provide(exchangerepo.New),
		fx.Provide(exchangeapp.NewService),
		fx.Invoke(func(
			info buildinfo.Info,
			readModel *uireadrepo.Repository,
			projects *project.Service,
			tasks *taskapp.Service,
			runs *runapp.Service,
			knowledge *knowledgeapp.Service,
			database *sql.DB,
			tasksRepo *taskrepo.Repository,
			builder *contextpackapp.Service,
			exchanges *exchangeapp.Service,
		) {
			services = webui.Services{Build: info, ReadModel: readModel, Projects: projects, Tasks: tasks, Runs: runs, Knowledge: knowledge, Exchange: exchanges}
			db = database
			taskRepository = tasksRepo
			contextBuilder = builder
		}),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Error(err)
		}
	})

	pastClock := func() time.Time { return time.Now().Add(-time.Hour) }
	f.handler = webui.NewTestHandler(testPort, services)
	f.projects = services.Projects
	f.tasks = services.Tasks
	f.runs = services.Runs
	f.staleRuns = runapp.NewService(runrepo.NewWithClock(db, pastClock), taskRepository, contextBuilder)

	return f
}

func (f *fixture) newProject(t *testing.T, name string) project.Project {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := f.projects.Init(context.Background(), resolved, name)
	if err != nil {
		t.Fatal(err)
	}

	return result.Project
}

func (f *fixture) newTask(t *testing.T, projectID, title string) task.Task {
	t.Helper()

	value, err := f.tasks.Create(context.Background(), task.CreateInput{ProjectID: projectID, Title: title}, testTaskActor)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func claim(t *testing.T, service *runapp.Service, value task.Task) run.Run {
	t.Helper()

	claimed, err := service.Claim(context.Background(), task.Selector{ProjectID: value.ProjectID, ID: value.ID}, run.ClaimInput{
		WithoutRetrieval:        true,
		RetrievalOverrideReason: "test without index",
	}, testRunActor)
	if err != nil {
		t.Fatal(err)
	}

	return claimed
}

type response struct {
	status int
	header http.Header
	body   []byte
}

type apiEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	Result        json.RawMessage `json:"result"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do sends a request with a valid loopback Host unless headers override it.
func (f *fixture) do(t *testing.T, method, target, body string, headers map[string]string) response {
	t.Helper()

	request := httptest.NewRequest(method, target, bytes.NewReader([]byte(body)))
	request.Host = "127.0.0.1:" + testPort
	for key, value := range headers {
		if key == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)

	return response{status: recorder.Code, header: recorder.Header(), body: recorder.Body.Bytes()}
}

func (f *fixture) doJSON(t *testing.T, method, target, body string) response {
	t.Helper()
	return f.do(t, method, target, body, map[string]string{"Content-Type": "application/json"})
}

func decode(t *testing.T, value response) apiEnvelope {
	t.Helper()

	var envelope apiEnvelope
	if err := json.Unmarshal(value.body, &envelope); err != nil {
		t.Fatalf("decode %d %s: %v", value.status, value.body, err)
	}
	if envelope.SchemaVersion != "1" {
		t.Fatalf("schema_version = %q in %s", envelope.SchemaVersion, value.body)
	}

	return envelope
}

func requireError(t *testing.T, value response, status int, code string) {
	t.Helper()

	envelope := decode(t, value)
	if value.status != status || envelope.Error == nil || envelope.Error.Code != code {
		t.Fatalf("response = %d %s, want %d %q", value.status, value.body, status, code)
	}
}

func requireResult(t *testing.T, value response, target any) {
	t.Helper()

	envelope := decode(t, value)
	if value.status != http.StatusOK || envelope.Error != nil {
		t.Fatalf("response = %d %s, want 200", value.status, value.body)
	}
	if target != nil {
		if err := json.Unmarshal(envelope.Result, target); err != nil {
			t.Fatalf("decode result %s: %v", envelope.Result, err)
		}
	}
}

func TestGuardRejectsForeignHosts(t *testing.T) {
	f := newFixture(t)

	for _, host := range []string{"evil.example:7700", "127.0.0.1:7701", "localhost", "127.0.0.1", "[::1]:7700", "0.0.0.0:7700", ""} {
		for _, path := range []string{"/api/v1/health", "/"} {
			got := f.do(t, http.MethodGet, path, "", map[string]string{"Host": host})
			if got.status != http.StatusForbidden || !strings.Contains(string(got.body), "forbidden host") {
				t.Fatalf("Host %q %s = %d %s, want 403 forbidden host", host, path, got.status, got.body)
			}
		}
	}

	for _, host := range []string{"127.0.0.1:7700", "localhost:7700"} {
		got := f.do(t, http.MethodGet, "/api/v1/health", "", map[string]string{"Host": host})
		requireResult(t, got, nil)

		if got.header.Get("X-Content-Type-Options") != "nosniff" || got.header.Get("X-Frame-Options") != "DENY" || got.header.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("Host %q security headers = %v", host, got.header)
		}
		if got.header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("Host %q sent CORS headers: %v", host, got.header)
		}
	}
}

func TestGuardRejectsForeignOriginsOnAPI(t *testing.T) {
	f := newFixture(t)

	foreign := []string{
		"http://evil.example",
		"https://127.0.0.1:7700",
		"http://127.0.0.1:7701",
		"http://localhost",
		"http://[::1]:7700",
		"null",
	}
	for _, origin := range foreign {
		got := f.do(t, http.MethodGet, "/api/v1/health", "", map[string]string{"Origin": origin})
		requireError(t, got, http.StatusForbidden, "forbidden_origin")

		got = f.do(t, http.MethodPost, "/api/v1/runs/x/abandon", `{}`, map[string]string{"Origin": origin, "Content-Type": "application/json"})
		requireError(t, got, http.StatusForbidden, "forbidden_origin")
	}

	for _, origin := range []string{"", "http://127.0.0.1:7700", "http://localhost:7700"} {
		got := f.do(t, http.MethodGet, "/api/v1/health", "", map[string]string{"Origin": origin})
		requireResult(t, got, nil)
	}

	// Static assets are not API: a foreign Origin there is not rejected.
	got := f.do(t, http.MethodGet, "/", "", map[string]string{"Origin": "http://evil.example"})
	if got.status != http.StatusOK {
		t.Fatalf("asset with foreign Origin = %d %s, want 200", got.status, got.body)
	}
}

func TestUnknownAPIPathIsNotFound(t *testing.T) {
	f := newFixture(t)

	for _, path := range []string{"/api/v1/nope", "/api/v2/projects", "/api/"} {
		requireError(t, f.do(t, http.MethodGet, path, "", nil), http.StatusNotFound, "not_found")
	}
}

func TestMutationBodyDecoding(t *testing.T) {
	f := newFixture(t)
	missing, err := run.NewID()
	if err != nil {
		t.Fatal(err)
	}
	target := "/api/v1/runs/" + missing + "/abandon"

	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "application/json-patch"} {
		got := f.do(t, http.MethodPost, target, `{"expected_revision":1}`, map[string]string{"Content-Type": contentType})
		requireError(t, got, http.StatusUnsupportedMediaType, "unsupported_media_type")
	}

	requireError(t, f.doJSON(t, http.MethodPost, target, `{"expected_revision":1,"surprise":true}`), http.StatusBadRequest, "invalid_argument")
	requireError(t, f.doJSON(t, http.MethodPost, target, `{"expected_revision":`), http.StatusBadRequest, "invalid_argument")
	requireError(t, f.doJSON(t, http.MethodPost, target, ``), http.StatusBadRequest, "invalid_argument")

	oversized := `{"expected_revision":1,"reason":"` + strings.Repeat("x", 64<<10) + `"}`
	requireError(t, f.doJSON(t, http.MethodPost, target, oversized), http.StatusBadRequest, "invalid_argument")

	// A JSON media type with parameters passes decoding and reaches the service.
	got := f.do(t, http.MethodPost, target, `{"expected_revision":1}`, map[string]string{"Content-Type": "application/json; charset=utf-8"})
	requireError(t, got, http.StatusNotFound, "not_found")

	requireError(t, f.do(t, http.MethodDelete, "/api/v1/projects/x", `{"expected_revision":1}`, map[string]string{"Content-Type": "text/plain"}), http.StatusUnsupportedMediaType, "unsupported_media_type")
	requireError(t, f.doJSON(t, http.MethodDelete, "/api/v1/projects/x", `{"expected_revision":1,"force":true}`), http.StatusBadRequest, "invalid_argument")
}

func TestAbandonRunOnlyClosesStaleRuns(t *testing.T) {
	f := newFixture(t)
	value := f.newProject(t, "abandon")

	liveTask := f.newTask(t, value.ID, "live")
	live := claim(t, f.runs, liveTask)

	got := f.doJSON(t, http.MethodPost, "/api/v1/runs/"+live.ID+"/abandon", `{"expected_revision":1}`)
	requireError(t, got, http.StatusConflict, "run_not_stale")
	if current, err := f.runs.GetRun(context.Background(), live.ID); err != nil || current.Status != run.StatusActive {
		t.Fatalf("live run after refused abandon = %+v, err=%v", current, err)
	}

	staleTask := f.newTask(t, value.ID, "stale")
	stale := claim(t, f.staleRuns, staleTask)
	if !stale.ExpiresAt.Before(time.Now()) {
		t.Fatalf("stale run lease expires at %v, want the past", stale.ExpiresAt)
	}
	target := "/api/v1/runs/" + stale.ID + "/abandon"

	got = f.doJSON(t, http.MethodPost, target, `{"expected_revision":99}`)
	requireError(t, got, http.StatusConflict, "revision_conflict")

	var abandoned run.Run
	got = f.doJSON(t, http.MethodPost, target, `{"expected_revision":`+itoa(stale.Revision)+`,"reason":"   "}`)
	requireResult(t, got, &abandoned)

	wantActor := run.ActorSnapshot{ID: "web-ui", Kind: "user", Name: "Web UI"}
	if abandoned.ID != stale.ID || abandoned.Status != run.StatusAbandoned || abandoned.Revision != stale.Revision+1 || abandoned.FinishedAt == nil {
		t.Fatalf("abandoned run = %+v", abandoned)
	}
	if abandoned.FinishedBy == nil || *abandoned.FinishedBy != wantActor {
		t.Fatalf("finished_by = %+v, want %+v", abandoned.FinishedBy, wantActor)
	}
	if !strings.HasPrefix(abandoned.ResultSummary, "Abandoned from the web UI") {
		t.Fatalf("result_summary = %q, want the default reason", abandoned.ResultSummary)
	}

	shown, err := f.tasks.Show(context.Background(), task.Selector{ProjectID: value.ID, ID: staleTask.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	var event *task.Event
	for i := range shown.Events {
		if shown.Events[i].Type == "run_abandoned" {
			event = &shown.Events[i]
		}
	}
	if event == nil || event.Actor.ID != wantActor.ID || event.Actor.Kind != wantActor.Kind || event.Actor.Name != wantActor.Name || !strings.Contains(event.Body, stale.ID) {
		t.Fatalf("task events = %+v, want run_abandoned by the web UI", shown.Events)
	}

	// The run is no longer active, so the stale check passes and the domain
	// refuses the transition itself.
	got = f.doJSON(t, http.MethodPost, target, `{"expected_revision":`+itoa(abandoned.Revision)+`}`)
	requireError(t, got, http.StatusConflict, "invalid_transition")

	reasonTask := f.newTask(t, value.ID, "reason")
	withReason := claim(t, f.staleRuns, reasonTask)
	got = f.doJSON(t, http.MethodPost, "/api/v1/runs/"+withReason.ID+"/abandon", `{"expected_revision":`+itoa(withReason.Revision)+`,"reason":"  agent crashed  "}`)
	requireResult(t, got, &abandoned)
	if abandoned.ResultSummary != "agent crashed" {
		t.Fatalf("result_summary = %q, want trimmed custom reason", abandoned.ResultSummary)
	}

	missing, err := run.NewID()
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, f.doJSON(t, http.MethodPost, "/api/v1/runs/"+missing+"/abandon", `{"expected_revision":1}`), http.StatusNotFound, "not_found")
}

func TestDeleteProjectRequiresRevisionAndSoftDeletes(t *testing.T) {
	f := newFixture(t)
	kept := f.newProject(t, "kept")
	doomed := f.newProject(t, "doomed")
	f.newTask(t, doomed.ID, "survives deletion")
	target := "/api/v1/projects/" + doomed.ID

	requireError(t, f.doJSON(t, http.MethodDelete, target, `{}`), http.StatusBadRequest, "invalid_argument")
	requireError(t, f.doJSON(t, http.MethodDelete, target, `{"expected_revision":0}`), http.StatusBadRequest, "invalid_argument")
	requireError(t, f.doJSON(t, http.MethodDelete, target, `{"expected_revision":`+itoa(doomed.Revision+5)+`}`), http.StatusConflict, "revision_conflict")

	missing, err := run.NewID()
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, f.doJSON(t, http.MethodDelete, "/api/v1/projects/"+missing, `{"expected_revision":1}`), http.StatusNotFound, "project_not_found")

	var deleted project.Project
	requireResult(t, f.doJSON(t, http.MethodDelete, target, `{"expected_revision":`+itoa(doomed.Revision)+`}`), &deleted)
	if deleted.ID != doomed.ID || deleted.DeletedAt == nil {
		t.Fatalf("deleted project = %+v", deleted)
	}

	var listed []project.Project
	requireResult(t, f.do(t, http.MethodGet, "/api/v1/projects", "", nil), &listed)
	if len(listed) != 1 || listed[0].ID != kept.ID {
		t.Fatalf("listed projects = %+v, want only %q", listed, kept.ID)
	}

	restorable, err := f.projects.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, value := range restorable {
		found = found || value.ID == doomed.ID
	}
	if !found {
		t.Fatalf("deleted projects = %+v, want soft-deleted %q", restorable, doomed.ID)
	}
}

func TestRunFeedValidatesQuery(t *testing.T) {
	f := newFixture(t)
	value := f.newProject(t, "feed")
	stale := claim(t, f.staleRuns, f.newTask(t, value.ID, "stale"))
	claim(t, f.runs, f.newTask(t, value.ID, "live"))

	invalid := []string{
		"lease=bogus",
		"lease=LIVE",
		"status=bogus",
		"status=active&status=bogus",
		"limit=0",
		"limit=-1",
		"limit=201",
		"limit=abc",
		"offset=-1",
		"offset=abc",
		"project=" + strings.Repeat("&project=x", 200),
	}
	for _, query := range invalid {
		requireError(t, f.do(t, http.MethodGet, "/api/v1/runs?"+query, "", nil), http.StatusBadRequest, "invalid_argument")
	}

	for _, query := range []string{"", "limit=1", "limit=200", "offset=0", "status=active&status=succeeded", "lease=live", "project=" + value.ID} {
		requireResult(t, f.do(t, http.MethodGet, "/api/v1/runs?"+query, "", nil), nil)
	}

	var items []uireadrepo.FeedItem
	requireResult(t, f.do(t, http.MethodGet, "/api/v1/runs?lease=expired&project="+value.ID, "", nil), &items)
	if len(items) != 1 || items[0].Run.ID != stale.ID || items[0].Project.Name != "feed" {
		t.Fatalf("expired feed = %+v, want stale run %q", items, stale.ID)
	}

	requireResult(t, f.do(t, http.MethodGet, "/api/v1/runs?limit=1", "", nil), &items)
	if len(items) != 1 {
		t.Fatalf("limited feed = %+v, want one item", items)
	}
}

func itoa(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

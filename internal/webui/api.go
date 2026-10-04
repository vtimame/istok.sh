package webui

import (
	"net/http"
	"strconv"

	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

const (
	defaultRunLimit = 100
	knowledgeLimit  = 200
)

// Services are the application services the read-only API reads from.
type Services struct {
	Build     buildinfo.Info
	Projects  *project.Service
	Tasks     *taskapp.Service
	Runs      *runapp.Service
	Knowledge *knowledgeapp.Service
}

type api struct {
	services Services
}

func (a api) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", a.health)
	mux.HandleFunc("GET /api/v1/projects", a.projects)
	mux.HandleFunc("GET /api/v1/projects/{project}/tasks", a.tasks)
	mux.HandleFunc("GET /api/v1/projects/{project}/tasks/{number}", a.task)
	mux.HandleFunc("GET /api/v1/projects/{project}/runs", a.runs)
	mux.HandleFunc("GET /api/v1/projects/{project}/knowledge", a.knowledge)
	mux.HandleFunc("GET /api/v1/runs/{run}", a.run)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
	})
}

func (a api) health(w http.ResponseWriter, _ *http.Request) {
	writeResult(w, map[string]string{
		"version": a.services.Build.Version,
		"commit":  a.services.Build.Commit,
	})
}

func (a api) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.services.Projects.List(r.Context(), false)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeResult(w, projects)
}

func (a api) tasks(w http.ResponseWriter, r *http.Request) {
	options := task.ListOptions{}
	for _, status := range r.URL.Query()["status"] {
		options.Statuses = append(options.Statuses, task.Status(status))
	}

	tasks, err := a.services.Tasks.List(r.Context(), r.PathValue("project"), options)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeResult(w, tasks)
}

func (a api) task(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(r.PathValue("number"), 10, 64)
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "task number must be a positive integer")
		return
	}

	selector := task.Selector{ProjectID: r.PathValue("project"), Number: number}
	shown, err := a.services.Tasks.Show(r.Context(), selector, false)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeResult(w, shown)
}

func (a api) runs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	options := run.ListOptions{
		ProjectID: r.PathValue("project"),
		TaskID:    query.Get("task_id"),
		Limit:     defaultRunLimit,
	}

	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_argument", "limit must be a positive integer")
			return
		}
		options.Limit = limit
	}
	for _, status := range query["status"] {
		options.Statuses = append(options.Statuses, run.Status(status))
	}

	runs, err := a.services.Runs.ListRuns(r.Context(), options)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeResult(w, runs)
}

func (a api) run(w http.ResponseWriter, r *http.Request) {
	shown, err := a.services.Runs.ShowRun(r.Context(), r.PathValue("run"))
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeResult(w, shown)
}

func (a api) knowledge(w http.ResponseWriter, r *http.Request) {
	items, err := a.services.Knowledge.Catalog(r.Context(), r.PathValue("project"), knowledge.CatalogOptions{Limit: knowledgeLimit})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeResult(w, items)
}

package webui

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"

	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/storage/taskrepo"
	"github.com/vtimame/istok.sh/internal/task"
)

const (
	defaultRunLimit = 100
	knowledgeLimit  = 200
)

// StatsReader summarizes tasks and runs per project.
type StatsReader interface {
	ProjectStats(ctx context.Context) (map[string]taskrepo.ProjectStats, error)
}

// Services are the application services the read-only API reads from.
type Services struct {
	Build     buildinfo.Info
	Stats     StatsReader
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
	mux.HandleFunc("GET /api/v1/projects/{project}/knowledge", a.knowledge) // ?q= switches to full-text search
	mux.HandleFunc("GET /api/v1/runs/{run}", a.run)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
	})
}

func (a api) health(w http.ResponseWriter, _ *http.Request) {
	// The home directory lets the UI shorten project paths to ~/...
	home, _ := os.UserHomeDir()

	writeResult(w, map[string]string{
		"version": a.services.Build.Version,
		"commit":  a.services.Build.Commit,
		"home":    home,
	})
}

// projectView adds task counts and the latest activity, which
// project.updated_at does not track: it changes only on rename, rebind and delete.
type projectView struct {
	project.Project
	Stats taskrepo.ProjectStats `json:"stats"`
}

func (a api) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.services.Projects.List(r.Context(), false)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	stats, err := a.services.Stats.ProjectStats(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	views := make([]projectView, 0, len(projects))
	for _, value := range projects {
		views = append(views, projectView{Project: value, Stats: stats[value.ID]})
	}

	writeResult(w, views)
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
	projectID := r.PathValue("project")
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	var (
		items []knowledge.CatalogItem
		err   error
	)
	if query == "" {
		items, err = a.services.Knowledge.Catalog(r.Context(), projectID, knowledge.CatalogOptions{Limit: knowledgeLimit})
	} else {
		items, err = a.services.Knowledge.Search(r.Context(), projectID, knowledge.SearchOptions{Query: query, Limit: knowledgeLimit})
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeResult(w, items)
}

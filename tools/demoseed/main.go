// demoseed builds a demo Istok database for screenshots and documentation.
//
// It creates fictional projects with small real source trees, then plays a
// scenario through the application services: Claude and Codex claim tasks,
// run checks, fail, hand over and finish, one run stays live and one goes
// stale. Everything lives under -root, separate from your real database.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	contextapp "github.com/vtimame/istok.sh/internal/application/context"
	contextpackapp "github.com/vtimame/istok.sh/internal/application/contextpack"
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/storage/runrepo"
	"github.com/vtimame/istok.sh/internal/storage/taskrepo"
)

func main() {
	root := flag.String("root", "", "directory for the demo database, indexes and projects (recreated)")
	flag.Parse()

	if *root == "" {
		fmt.Fprintln(os.Stderr, "demoseed: -root is required")
		os.Exit(2)
	}

	absolute, err := filepath.Abs(*root)
	check(err)
	check(os.RemoveAll(absolute))

	// Projects live under <root>/home/work, so a UI started with HOME=<root>/home
	// shows them as ~/work/<name>.
	home := filepath.Join(absolute, "home")
	workRoot := filepath.Join(home, "work")
	check(os.MkdirAll(workRoot, 0o755))
	workRoot, err = filepath.EvalSymlinks(workRoot)
	check(err)

	database := filepath.Join(absolute, "istok.db")
	check(os.Setenv("ISTOK_INDEX_ROOT", filepath.Join(absolute, "indexes")))

	seed(database, workRoot)

	fmt.Printf("Demo database: %s\n\nStart the UI on it:\n\n  HOME=%s istok ui --database %s --port 7710\n", database, home, database)
}

func seed(database, workRoot string) {
	ctx := context.Background()
	w := &world{ctx: ctx, workRoot: workRoot, now: time.Now()}

	var tasks *taskrepo.Repository
	var builder *contextpackapp.Service
	app := fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		bootstrap.TaskOptions(database),
		fx.Invoke(func(
			db *sql.DB,
			projects *project.Service,
			taskService *taskapp.Service,
			taskRepository *taskrepo.Repository,
			contextBuilder *contextpackapp.Service,
			indexing *indexingapp.Service,
			contexts *contextapp.Service,
			knowledge *knowledgeapp.Service,
		) {
			w.db, w.projects, w.tasks = db, projects, taskService
			w.indexing, w.contexts, w.knowledge = indexing, contexts, knowledge
			tasks, builder = taskRepository, contextBuilder
		}),
	)
	check(app.Start(ctx))
	defer app.Stop(ctx) //nolint:errcheck

	// Runs use the scenario clock, so they spread over the last days.
	w.runs = runapp.NewService(runrepo.NewWithClock(w.db, w.clock), tasks, builder)

	seedScenario(w)
}

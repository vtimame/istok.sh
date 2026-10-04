// uiload measures how web UI reads affect agent writes on a SQLite database.
//
// It runs a writer that behaves like an agent's MCP server (run heartbeats and
// task progress through the application services, in its own process and
// connection pool) while simulated browser tabs poll a real `istok ui` server
// over HTTP. Run it against a copy of a real database, never the live one.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type scenario struct {
	name     string
	tabs     int
	interval time.Duration
}

var (
	taskActor = task.ActorSnapshot{ID: "uiload", Kind: "agent", Name: "UI load writer"}
	runActor  = run.ActorSnapshot{ID: "uiload", Kind: "agent", Name: "UI load writer"}
)

func main() {
	database := flag.String("database", "", "copy of an istok database to measure against")
	binary := flag.String("istok", "bin/istok", "istok binary that serves the UI")
	duration := flag.Duration("duration", 45*time.Second, "duration of each scenario")
	writeEvery := flag.Duration("write-every", 100*time.Millisecond, "pause between agent writes")
	port := flag.Int("port", 7790, "loopback port for the measured UI server")
	flag.Parse()

	if *database == "" {
		fmt.Fprintln(os.Stderr, "uiload: -database is required")
		os.Exit(2)
	}

	scenarios := []scenario{
		{name: "no UI", tabs: 0},
		{name: "1 tab, 5s polling", tabs: 1, interval: 5 * time.Second},
		{name: "5 tabs, 1s polling", tabs: 5, interval: time.Second},
	}

	if err := execute(*database, *binary, *duration, *writeEvery, *port, scenarios); err != nil {
		fmt.Fprintln(os.Stderr, "uiload:", err)
		os.Exit(1)
	}
}

func execute(database, binary string, duration, writeEvery time.Duration, port int, scenarios []scenario) error {
	ctx := context.Background()
	os.Setenv("ISTOK_INDEX_ROOT", filepath.Join(os.TempDir(), "uiload-index"))

	var tasks *taskapp.Service
	var runs *runapp.Service
	var projects *project.Service
	app := fx.New(
		fx.NopLogger,
		fx.Supply(buildinfo.Current()),
		bootstrap.TaskOptions(database),
		fx.Invoke(func(p *project.Service, t *taskapp.Service, r *runapp.Service) {
			projects, tasks, runs = p, t, r
		}),
	)
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start writer services: %w", err)
	}
	defer app.Stop(ctx) //nolint:errcheck

	writer, err := newWriter(ctx, projects, tasks, runs)
	if err != nil {
		return err
	}

	// Pick the busiest project and a run with a snapshot, so reads hit the
	// heaviest pages the UI really serves.
	readProject, readRun, err := pickReadTargets(ctx, projects, tasks, runs)
	if err != nil {
		return err
	}
	endpoints := []string{
		"/api/v1/projects",
		"/api/v1/runs?limit=50",
		"/api/v1/projects/" + readProject + "/tasks",
		"/api/v1/runs/" + readRun,
	}

	fmt.Printf("database: %s\nwrites every %s, %s per scenario\n\n", database, writeEvery, duration)
	fmt.Printf("%-20s %7s %8s %8s %8s %8s %7s | %7s %8s %8s %6s\n",
		"scenario", "writes", "w p50", "w p95", "w p99", "w max", "w errs", "reads", "r p50", "r p95", "r errs")

	for _, value := range scenarios {
		result, err := measure(ctx, value, writer, binary, database, port, endpoints, duration, writeEvery)
		if err != nil {
			return fmt.Errorf("%s: %w", value.name, err)
		}

		fmt.Printf("%-20s %7d %8s %8s %8s %8s %7d | %7d %8s %8s %6d\n",
			value.name, len(result.writes), pct(result.writes, 50), pct(result.writes, 95), pct(result.writes, 99), pct(result.writes, 100), result.writeErrors,
			len(result.reads), pct(result.reads, 50), pct(result.reads, 95), result.readErrors)
		for _, message := range result.errorSamples {
			fmt.Printf("    error: %s\n", message)
		}
	}

	return nil
}

type writer struct {
	tasks    *taskapp.Service
	runs     *runapp.Service
	selector task.Selector
	revision int64
	run      run.Run
	step     int
}

func newWriter(ctx context.Context, projects *project.Service, tasks *taskapp.Service, runs *runapp.Service) (*writer, error) {
	root, err := os.MkdirTemp("", "uiload-project-")
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}

	created, err := projects.Init(ctx, root, "uiload")
	if err != nil {
		return nil, fmt.Errorf("init load project: %w", err)
	}
	value, err := tasks.Create(ctx, task.CreateInput{ProjectID: created.Project.ID, Title: "UI load writer task"}, taskActor)
	if err != nil {
		return nil, fmt.Errorf("create load task: %w", err)
	}

	selector := task.Selector{ProjectID: value.ProjectID, ID: value.ID}
	claimed, err := runs.Claim(ctx, selector, run.ClaimInput{
		WithoutRetrieval:        true,
		RetrievalOverrideReason: "uiload measures storage contention, not retrieval",
	}, runActor)
	if err != nil {
		return nil, fmt.Errorf("claim load task: %w", err)
	}

	shown, err := tasks.Show(ctx, selector, false)
	if err != nil {
		return nil, err
	}

	return &writer{tasks: tasks, runs: runs, selector: selector, revision: shown.Task.Revision, run: claimed}, nil
}

// write alternates the two most frequent agent writes: a run heartbeat and a
// task progress note.
func (w *writer) write(ctx context.Context) error {
	w.step++
	if w.step%2 == 0 {
		_, err := w.runs.Heartbeat(ctx, run.HeartbeatInput{RunID: w.run.ID, LeaseID: w.run.LeaseID, LeaseDuration: 15 * time.Minute}, runActor)
		return err
	}

	updated, err := w.tasks.Progress(ctx, w.selector, w.revision, fmt.Sprintf("load step %d", w.step), taskActor)
	if err != nil {
		return err
	}
	w.revision = updated.Revision

	return nil
}

func pickReadTargets(ctx context.Context, projects *project.Service, tasks *taskapp.Service, runs *runapp.Service) (string, string, error) {
	values, err := projects.List(ctx, false)
	if err != nil {
		return "", "", err
	}

	best, bestCount := "", -1
	for _, value := range values {
		items, err := tasks.List(ctx, value.ID, task.ListOptions{})
		if err != nil {
			return "", "", err
		}
		if len(items) > bestCount {
			best, bestCount = value.ID, len(items)
		}
	}

	recent, err := runs.ListRuns(ctx, run.ListOptions{ProjectID: best, Limit: 1})
	if err != nil || len(recent) == 0 {
		return "", "", fmt.Errorf("no run to read in project %s: %v", best, err)
	}

	return best, recent[0].ID, nil
}

type result struct {
	writes       []time.Duration
	reads        []time.Duration
	writeErrors  int
	readErrors   int
	errorSamples []string
}

func measure(ctx context.Context, value scenario, writer *writer, binary, database string, port int, endpoints []string, duration, writeEvery time.Duration) (*result, error) {
	var res result
	var mu sync.Mutex
	record := func(target *[]time.Duration, elapsed time.Duration, err error, counter *int) {
		mu.Lock()
		defer mu.Unlock()

		*target = append(*target, elapsed)
		if err != nil {
			*counter++
			if len(res.errorSamples) < 3 {
				res.errorSamples = append(res.errorSamples, err.Error())
			}
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var wg sync.WaitGroup
	if value.tabs > 0 {
		base, stop, err := startUI(binary, database, port)
		if err != nil {
			return nil, err
		}
		defer stop()

		client := &http.Client{Timeout: 30 * time.Second}
		for tab := 0; tab < value.tabs; tab++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pollTab(runCtx, client, base, endpoints, value.interval, func(elapsed time.Duration, err error) {
					record(&res.reads, elapsed, err, &res.readErrors)
				})
			}()
		}
	}

	ticker := time.NewTicker(writeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-runCtx.Done():
			wg.Wait()
			return &res, nil
		case <-ticker.C:
			started := time.Now()
			err := writer.write(ctx)
			record(&res.writes, time.Since(started), err, &res.writeErrors)
		}
	}
}

// pollTab mimics one open page: TanStack Query refetches its queries in
// parallel on every interval.
func pollTab(ctx context.Context, client *http.Client, base string, endpoints []string, interval time.Duration, record func(time.Duration, error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		var wg sync.WaitGroup
		for _, endpoint := range endpoints {
			wg.Add(1)
			go func() {
				defer wg.Done()
				started := time.Now()
				err := get(ctx, client, base+endpoint)
				if ctx.Err() == nil {
					record(time.Since(started), err)
				}
			}()
		}
		wg.Wait()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func get(ctx context.Context, client *http.Client, url string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, response.Status)
	}

	return nil
}

// startUI runs the real istok ui server in its own process, like the systemd
// service, and waits until it prints its address.
func startUI(binary, database string, port int) (string, func(), error) {
	command := exec.Command(binary, "ui", "--no-open", "--port", fmt.Sprint(port), "--database", database)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", nil, err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return "", nil, fmt.Errorf("start istok ui: %w", err)
	}
	stop := func() {
		_ = command.Process.Signal(os.Interrupt)
		_ = command.Wait()
	}

	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		if address, found := strings.CutPrefix(lines.Text(), "Istok UI: "); found {
			go func() { _, _ = io.Copy(io.Discard, stdout) }()
			return strings.TrimSuffix(address, "/"), stop, nil
		}
	}

	stop()
	return "", nil, errors.New("istok ui exited before printing its address")
}

func pct(values []time.Duration, percentile int) string {
	if len(values) == 0 {
		return "-"
	}

	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	index := (len(sorted)*percentile + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}

	return sorted[index-1].Round(100 * time.Microsecond).String()
}

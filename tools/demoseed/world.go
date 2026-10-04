package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	contextapp "github.com/vtimame/istok.sh/internal/application/context"
	indexingapp "github.com/vtimame/istok.sh/internal/application/indexing"
	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	runapp "github.com/vtimame/istok.sh/internal/application/run"
	taskapp "github.com/vtimame/istok.sh/internal/application/task"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

type agent struct {
	task      task.ActorSnapshot
	run       run.ActorSnapshot
	knowledge contextmodel.ActorSnapshot
}

func newAgent(id, kind, name string) agent {
	return agent{
		task:      task.ActorSnapshot{ID: id, Kind: kind, Name: name},
		run:       run.ActorSnapshot{ID: id, Kind: kind, Name: name},
		knowledge: contextmodel.ActorSnapshot{ID: id, Kind: kind, Name: name},
	}
}

var (
	claude = newAgent("claude", "agent", "Claude")
	codex  = newAgent("codex", "agent", "Codex")
	alex   = newAgent("alex", "user", "Alex")
)

// world drives the application services the way agents and people would,
// with a scenario clock for the run repository so runs spread over days.
type world struct {
	ctx       context.Context
	db        *sql.DB
	workRoot  string
	projects  *project.Service
	tasks     *taskapp.Service
	runs      *runapp.Service
	indexing  *indexingapp.Service
	contexts  *contextapp.Service
	knowledge *knowledgeapp.Service

	// now is the scenario time; every read advances it a little so events keep
	// a stable order.
	now time.Time
}

func (w *world) clock() time.Time {
	w.now = w.now.Add(time.Second)
	return w.now
}

func (w *world) at(moment time.Time) {
	w.now = moment
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

type demoProject struct {
	project.Project
	baseCommit string
}

func (w *world) newProject(name string, createdAt time.Time) demoProject {
	root := filepath.Join(w.workRoot, name)
	check(os.MkdirAll(root, 0o755))
	commit, err := writeRepo(root, name)
	check(err)

	created, err := w.projects.Init(w.ctx, root, name)
	check(err)
	_, err = w.indexing.Rebuild(w.ctx, created.Project)
	check(err)

	w.exec(`UPDATE projects SET created_at = ?, updated_at = ? WHERE id = ?`, stamp(createdAt), stamp(createdAt), created.Project.ID)
	return demoProject{Project: created.Project, baseCommit: commit}
}

type taskSpec struct {
	title, description, criteria string
}

func (w *world) newTask(p demoProject, spec taskSpec, by agent, createdAt time.Time) task.Task {
	value, err := w.tasks.Create(w.ctx, task.CreateInput{
		ProjectID:          p.ID,
		Title:              spec.title,
		Description:        spec.description,
		AcceptanceCriteria: spec.criteria,
	}, by.task)
	check(err)

	w.backdateLastEvent(value.ID, createdAt)
	w.exec(`UPDATE tasks SET created_at = ? WHERE id = ?`, stamp(createdAt), value.ID)
	return value
}

func selector(value task.Task) task.Selector {
	return task.Selector{ProjectID: value.ProjectID, ID: value.ID}
}

func (w *world) revision(value task.Task) int64 {
	shown, err := w.tasks.Show(w.ctx, selector(value), false)
	check(err)
	return shown.Task.Revision
}

func (w *world) progress(value task.Task, by agent, body string, moment time.Time) {
	_, err := w.tasks.Progress(w.ctx, selector(value), w.revision(value), body, by.task)
	check(err)
	w.backdateLastEvent(value.ID, moment)
}

func (w *world) comment(value task.Task, by agent, body string, moment time.Time) {
	_, err := w.tasks.Comment(w.ctx, selector(value), w.revision(value), body, by.task)
	check(err)
	w.backdateLastEvent(value.ID, moment)
}

func (w *world) block(value task.Task, by agent, reason string, moment time.Time) {
	_, err := w.tasks.Block(w.ctx, selector(value), w.revision(value), reason, by.task)
	check(err)
	w.backdateLastEvent(value.ID, moment)
}

func (w *world) dependsOn(blocked, blocker task.Task, by agent, moment time.Time) {
	_, err := w.tasks.AddDependency(w.ctx, selector(blocker), selector(blocked), w.revision(blocked), by.task)
	check(err)
	w.backdateLastEvent(blocked.ID, moment)
}

// claim starts a run with retrieval, so its snapshot carries real code.
func (w *world) claim(p demoProject, value task.Task, by agent, moment time.Time) run.Run {
	w.at(moment)
	claimed, err := w.runs.Claim(w.ctx, selector(value), run.ClaimInput{BaseBranch: "main", BaseCommit: p.baseCommit}, by.run)
	check(err)
	return claimed
}

type checkSpec struct {
	argv     []string
	exitCode int
	duration time.Duration
	summary  string
}

// verify records an execution and its validation the way an agent does after
// running a command. It returns the validation id.
func (w *world) verify(p demoProject, current run.Run, by agent, spec checkSpec) string {
	started, err := w.runs.StartExecution(w.ctx, run.StartExecutionInput{
		RunID:   current.ID,
		LeaseID: current.LeaseID,
		Argv:    spec.argv,
		CWD:     filepath.Join(w.workRoot, p.Name),
	}, by.run)
	check(err)

	w.now = w.now.Add(spec.duration)
	exitCode := spec.exitCode
	duration := spec.duration.Milliseconds()
	status := run.ExecutionSucceeded
	validation := run.ValidationStatusPassed
	if exitCode != 0 {
		status = run.ExecutionFailed
		validation = run.ValidationStatusFailed
	}

	finished, err := w.runs.FinishExecution(w.ctx, run.FinishExecutionInput{
		ExecutionID:      started.ID,
		ExpectedRevision: started.Revision,
		Status:           status,
		ExitCode:         &exitCode,
		DurationMS:       &duration,
	}, by.run)
	check(err)

	recorded, err := w.runs.RecordValidation(w.ctx, run.RecordValidationInput{
		ExecutionID: finished.ID,
		Source:      run.ValidationSourceAttested,
		Status:      validation,
		Command:     joinArgs(spec.argv),
		ExitCode:    &exitCode,
		DurationMS:  &duration,
		Summary:     spec.summary,
	}, by.run)
	check(err)

	return recorded.ID
}

func joinArgs(argv []string) string {
	result := ""
	for i, arg := range argv {
		if i > 0 {
			result += " "
		}
		result += arg
	}
	return result
}

// finish closes the run a couple of minutes after its last check, inside the
// lease, like an agent wrapping up.
func (w *world) finish(current run.Run, by agent, status run.Status, summary string) run.Run {
	w.now = w.now.Add(2 * time.Minute)
	latest, err := w.runs.GetRun(w.ctx, current.ID)
	check(err)

	finished, err := w.runs.FinishRun(w.ctx, run.FinishRunInput{
		RunID:            current.ID,
		LeaseID:          latest.LeaseID,
		ExpectedRevision: latest.Revision,
		Status:           status,
		ResultSummary:    summary,
	}, by.run)
	check(err)
	return finished
}

func (w *world) complete(value task.Task, current run.Run, validationID string, by agent, note string) {
	w.now = w.now.Add(time.Minute)
	_, _, err := w.runs.CompleteTask(w.ctx, selector(value), run.CompleteTaskInput{
		ExpectedTaskRevision: w.revision(value),
		RunID:                current.ID,
		ValidationID:         validationID,
		Note:                 note,
	}, by.run)
	check(err)
}

// keepAlive extends a live run's lease so it shows as "agent working" for the
// whole screenshot session.
func (w *world) keepAlive(current run.Run, by agent, lease time.Duration) {
	w.at(time.Now())
	_, err := w.runs.Heartbeat(w.ctx, run.HeartbeatInput{RunID: current.ID, LeaseID: current.LeaseID, LeaseDuration: lease}, by.run)
	check(err)
}

func (w *world) instruction(p demoProject, kind contextmodel.Kind, delivery contextmodel.Delivery, title, body string, tags ...string) {
	_, err := w.contexts.Create(w.ctx, contextmodel.CreateInput{
		ProjectID:   p.ID,
		Kind:        kind,
		Title:       title,
		Body:        body,
		Tags:        tags,
		Source:      contextmodel.SourceUser,
		Visibility:  contextmodel.VisibilityShared,
		Sensitivity: contextmodel.SensitivityNormal,
		Delivery:    delivery,
	}, alex.knowledge)
	check(err)
}

type knowledgeSpec struct {
	kind                  knowledge.Kind
	title, summary, body  string
	tags                  []string
	promote               bool
	createdAt, reviewedAt time.Time
	sourceTask            task.Task
}

func (w *world) knowledgeItem(p demoProject, by agent, spec knowledgeSpec) {
	item, err := w.knowledge.Distill(w.ctx, knowledge.CreateInput{
		ProjectID:   p.ID,
		Kind:        spec.kind,
		Title:       spec.title,
		Summary:     spec.summary,
		Body:        spec.body,
		Tags:        spec.tags,
		Visibility:  contextmodel.VisibilityShared,
		Sensitivity: contextmodel.SensitivityNormal,
		Provenance:  []knowledge.Provenance{{Type: knowledge.ProvenanceTask, ID: spec.sourceTask.ID}},
	}, by.knowledge)
	check(err)

	updatedAt := spec.createdAt
	if spec.promote {
		reviewed, err := w.knowledge.Review(w.ctx, item.ID, item.Revision, "Checked against the current code.", alex.knowledge)
		check(err)
		_, err = w.knowledge.Promote(w.ctx, item.ID, reviewed.Revision, alex.knowledge)
		check(err)
		updatedAt = spec.reviewedAt
		w.exec(`UPDATE knowledge_items SET reviewed_at = ? WHERE id = ?`, stamp(spec.reviewedAt), item.ID)
	}

	w.exec(`UPDATE knowledge_items SET created_at = ?, updated_at = ? WHERE id = ?`, stamp(spec.createdAt), stamp(updatedAt), item.ID)
}

// The task repository stamps rows with the wall clock. For a demo database the
// scenario times are applied afterwards with plain SQL; nothing else relies
// on these columns.

func (w *world) backdateLastEvent(taskID string, moment time.Time) {
	w.exec(`UPDATE task_events SET created_at = ? WHERE rowid = (SELECT MAX(rowid) FROM task_events WHERE task_id = ?)`, stamp(moment), taskID)
}

func (w *world) settleTaskTimes() {
	w.exec(`UPDATE tasks SET updated_at = (SELECT MAX(created_at) FROM task_events WHERE task_id = tasks.id)
		WHERE EXISTS (SELECT 1 FROM task_events WHERE task_id = tasks.id)`)
}

func (w *world) exec(query string, args ...any) {
	if _, err := w.db.ExecContext(w.ctx, query, args...); err != nil {
		panic(fmt.Errorf("%s: %w", query, err))
	}
}

func stamp(moment time.Time) string {
	return moment.UTC().Format(time.RFC3339Nano)
}

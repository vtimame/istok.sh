// Package exchangerepo exports and imports project data as an exchange bundle.
// It works on database rows directly: the bundle mirrors the schema of one
// migration version, and both sides must run the same version.
package exchangerepo

// kind decides how an imported row merges with the local database.
type kind int

const (
	// mutable rows carry a revision: a higher incoming revision replaces the
	// local row, an equal one with different content is a conflict that keeps
	// the local row.
	mutable kind = iota

	// appendOnly rows never change after insert: missing rows are added.
	appendOnly

	// followsParent rows are replaced together with their parent row: run
	// leases follow runs, dependencies follow the blocked task.
	followsParent
)

// parent links a column to the table whose rows it references, so a row whose
// parent was skipped is skipped too.
type parent struct {
	column string
	table  string
}

// table describes one exported table.
type table struct {
	name string
	kind kind

	// key is the primary key. A composite key joins its values with "/".
	key []string

	// exclude lists columns that never leave this device.
	exclude []string

	// keepLocal lists columns an update never overwrites.
	keepLocal []string

	// owner is the parent column whose replacement replaces followsParent rows.
	owner string

	parents []parent

	// where filters exported rows. {projects} expands to the selected project
	// ID placeholders.
	where string
}

// tables are listed in import order: every table comes after the tables its
// rows reference.
var tables = []table{
	{
		name:    "projects",
		kind:    mutable,
		key:     []string{"id"},
		exclude: []string{"deleted_at"},
		where:   "id IN ({projects})",
	},
	{
		name:      "tasks",
		kind:      mutable,
		key:       []string{"id"},
		keepLocal: []string{"number"},
		parents:   []parent{{"project_id", "projects"}},
		where:     "project_id IN ({projects})",
	},
	{
		name:    "task_events",
		kind:    appendOnly,
		key:     []string{"id"},
		parents: []parent{{"task_id", "tasks"}},
		where:   "task_id IN (SELECT id FROM tasks WHERE project_id IN ({projects}))",
	},
	{
		name:    "task_dependencies",
		kind:    followsParent,
		key:     []string{"id"},
		owner:   "blocked_task_id",
		parents: []parent{{"blocker_task_id", "tasks"}, {"blocked_task_id", "tasks"}},
		where:   "project_id IN ({projects})",
	},
	{
		name:    "context_records",
		kind:    mutable,
		key:     []string{"id"},
		parents: []parent{{"project_id", "projects"}},
		where:   "project_id IN ({projects}) AND visibility = 'shared'",
	},
	{
		name:    "context_events",
		kind:    appendOnly,
		key:     []string{"id"},
		parents: []parent{{"record_id", "context_records"}},
		where:   "record_id IN (SELECT id FROM context_records WHERE project_id IN ({projects}) AND visibility = 'shared')",
	},
	{
		name:    "knowledge_items",
		kind:    mutable,
		key:     []string{"id"},
		parents: []parent{{"project_id", "projects"}},
		where:   "project_id IN ({projects}) AND visibility = 'shared'",
	},
	{
		name:    "knowledge_events",
		kind:    appendOnly,
		key:     []string{"id"},
		parents: []parent{{"item_id", "knowledge_items"}},
		where:   "item_id IN (SELECT id FROM knowledge_items WHERE project_id IN ({projects}) AND visibility = 'shared')",
	},
	{
		name:    "context_snapshots",
		kind:    appendOnly,
		key:     []string{"id"},
		parents: []parent{{"project_id", "projects"}},
		where:   "project_id IN ({projects})",
	},
	{
		name:    "context_snapshot_items",
		kind:    appendOnly,
		key:     []string{"snapshot_id", "position"},
		parents: []parent{{"snapshot_id", "context_snapshots"}},
		where:   "visibility = 'shared' AND snapshot_id IN (SELECT id FROM context_snapshots WHERE project_id IN ({projects}))",
	},
	{
		name:    "context_snapshot_retrieval_metadata",
		kind:    appendOnly,
		key:     []string{"snapshot_id"},
		parents: []parent{{"snapshot_id", "context_snapshots"}},
		where:   "snapshot_id IN (SELECT id FROM context_snapshots WHERE project_id IN ({projects}))",
	},
	{
		name:    "context_snapshot_assembly_metadata",
		kind:    appendOnly,
		key:     []string{"snapshot_id"},
		parents: []parent{{"snapshot_id", "context_snapshots"}},
		where:   "snapshot_id IN (SELECT id FROM context_snapshots WHERE project_id IN ({projects}))",
	},
	{
		name:    "runs",
		kind:    mutable,
		key:     []string{"id"},
		parents: []parent{{"task_id", "tasks"}, {"context_snapshot_id", "context_snapshots"}},
		where:   "task_id IN (SELECT id FROM tasks WHERE project_id IN ({projects}))",
	},
	{
		name:    "run_leases",
		kind:    followsParent,
		key:     []string{"run_id"},
		owner:   "run_id",
		parents: []parent{{"run_id", "runs"}},
		where:   "run_id IN (SELECT r.id FROM runs r JOIN tasks t ON t.id = r.task_id WHERE t.project_id IN ({projects}))",
	},
	{
		name:    "executions",
		kind:    mutable,
		key:     []string{"id"},
		parents: []parent{{"run_id", "runs"}},
		where:   "run_id IN (SELECT r.id FROM runs r JOIN tasks t ON t.id = r.task_id WHERE t.project_id IN ({projects}))",
	},
	{
		name:    "validations",
		kind:    appendOnly,
		key:     []string{"id"},
		parents: []parent{{"execution_id", "executions"}},
		where: "execution_id IN (SELECT e.id FROM executions e JOIN runs r ON r.id = e.run_id " +
			"JOIN tasks t ON t.id = r.task_id WHERE t.project_id IN ({projects}))",
	},
	{
		name:    "task_completions",
		kind:    appendOnly,
		key:     []string{"id"},
		parents: []parent{{"task_id", "tasks"}, {"run_id", "runs"}, {"validation_id", "validations"}},
		where:   "task_id IN (SELECT id FROM tasks WHERE project_id IN ({projects}))",
	},
}

// Excluded data, for reference: project_roots (paths are per device),
// project_task_counters (recomputed on import), artifacts (their files stay
// on the device that ran the command), context_snapshot_retrieval_items (local
// code snippets) and goose_db_version.

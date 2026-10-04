// Mirrors the JSON tags of the Go domain types served by /api/v1.

// Go encodes nil slices as null, so every array from the API may be null.
export type List<T> = T[] | null

export function list<T>(value: List<T> | undefined): T[] {
  return value ?? []
}

export type Actor = {
  id: string
  kind: string
  name: string
}

export type Project = {
  id: string
  name: string
  revision: number
  created_at: string
  updated_at: string
  deleted_at?: string
  root?: { canonical_path: string; active_at: string; detached_at?: string }
  stats: ProjectStats
}

export type ProjectStats = {
  open: number
  blocked: number
  done: number
  active_runs: number
  // Active runs whose lease expired: the agent stopped heartbeating.
  stale_runs: number
  // Latest task or run update; absent when the project has no tasks.
  last_activity_at?: string
}

export type Health = {
  version: string
  commit: string
  home: string
}

export type TaskStatus = "open" | "blocked" | "done"

export type Task = {
  id: string
  project_id: string
  number: number
  revision: number
  status: TaskStatus
  title: string
  description: string
  acceptance_criteria: string
  notes: string
  created_at: string
  updated_at: string
  deleted_at?: string
}

export type TaskListItem = Task & {
  has_active_run: boolean
  has_expired_run: boolean
  active_blockers?: List<{ id: string; number: number; title: string }>
}

export type TaskEvent = {
  id: string
  task_id: string
  type: string
  body: string
  task_revision: number
  actor: Actor
  created_at: string
}

export type TaskSummary = {
  id: string
  number: number
  status: TaskStatus
  title: string
  deleted_at?: string
}

export type TaskShow = {
  task: Task
  events: List<TaskEvent>
  has_active_run: boolean
  has_expired_run: boolean
  blockers: List<TaskSummary>
  dependents: List<TaskSummary>
}

export type RunStatus =
  "active" | "succeeded" | "failed" | "blocked" | "cancelled" | "abandoned"

export type Run = {
  id: string
  task_id: string
  context_snapshot_id: string
  revision: number
  status: RunStatus
  actor: Actor
  base_branch: string
  base_commit: string
  started_at: string
  updated_at: string
  finished_at?: string
  finished_by?: Actor
  result_summary: string
  validation_override?: string
  lease_owner: Actor
  heartbeat_at: string
  expires_at: string
}

export type Execution = {
  id: string
  run_id: string
  status: "running" | "succeeded" | "failed" | "cancelled"
  argv: List<string>
  cwd: string
  exit_code?: number
  duration_ms?: number
  signal: string
  timed_out: boolean
  actor: Actor
  started_at: string
  finished_at?: string
}

export type Validation = {
  id: string
  execution_id: string
  source: string
  status: "passed" | "failed"
  command: string
  exit_code?: number
  duration_ms?: number
  summary: string
  actor: Actor
  created_at: string
}

export type SnapshotRecord = {
  record_id: string
  kind: string
  delivery?: string
  title: string
  snippet: string
  tags: List<string>
  lane?: string
  score?: number
  reasons?: List<string>
}

export type SnapshotRetrieval = {
  item_id: string
  kind: string
  path: string
  line_start: number
  line_end: number
  symbol?: string
  language?: string
  score: number
  reasons?: List<string>
  snippet: string
}

export type ContextSnapshot = {
  id: string
  schema_version: string
  project_id: string
  generated_at: string
  records: List<SnapshotRecord>
  retrieval: List<SnapshotRetrieval>
  knowledge_catalog: List<{ id: string; title: string; summary?: string }>
  metadata: {
    assembly?: {
      total_budget_bytes?: number
      usage?: {
        total_bytes?: number
        durable_bytes?: number
        retrieval_bytes?: number
      }
      warnings?: List<string>
    }
    without_retrieval?: boolean
    override_reason?: string
  }
}

export type RunShow = {
  run: Run
  snapshot: ContextSnapshot
  executions: List<Execution>
  validations: List<Validation>
}

export type KnowledgeItem = {
  id: string
  revision: number
  kind: string
  status: string
  title: string
  summary: string
  snippet: string
  tags: List<string>
  reviewed_at?: string
  updated_at: string
}

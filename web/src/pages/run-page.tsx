import type { ReactNode } from "react"
import { ChevronRight } from "lucide-react"
import { Link, useParams } from "react-router"

import { InlineCode, Markdown } from "@/components/markdown"
import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { RunStateBadge, StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useProject, useRun, useTasks } from "@/hooks/queries"
import { useNow } from "@/hooks/use-now"
import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatRelative,
} from "@/lib/format"
import { isRunStale, runDurationMs } from "@/lib/runs"
import { list } from "@/lib/types"
import type { ContextSnapshot, Execution, Run, Validation } from "@/lib/types"

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="truncate text-sm">{value || "—"}</span>
    </div>
  )
}

function Empty({ children }: { children: ReactNode }) {
  return (
    <p className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
      {children}
    </p>
  )
}

function ValidationList({ validations }: { validations: Validation[] }) {
  if (validations.length === 0) {
    return (
      <Empty>
        No validation recorded. A run should record the checks that prove its
        result.
      </Empty>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {validations.map((validation) => (
        <div
          key={validation.id}
          className="flex flex-col gap-2 rounded-md border p-3"
        >
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge status={validation.status} />
            <Badge variant="outline" className="font-normal">
              {validation.source}
            </Badge>
            <span className="text-xs text-muted-foreground">
              {validation.actor.name} · {formatRelative(validation.created_at)}
            </span>
          </div>
          <code className="rounded bg-muted px-2 py-1 font-mono text-xs break-all">
            {validation.command}
          </code>
          {validation.summary && <Markdown>{validation.summary}</Markdown>}
        </div>
      ))}
    </div>
  )
}

function ExecutionList({ executions }: { executions: Execution[] }) {
  if (executions.length === 0) {
    return <Empty>No executions recorded.</Empty>
  }

  return (
    <div className="flex flex-col gap-3">
      {executions.map((execution) => (
        <div
          key={execution.id}
          className="flex flex-col gap-2 rounded-md border p-3"
        >
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge status={execution.status} />
            {execution.exit_code !== undefined && (
              <Badge variant="outline" className="font-normal">
                exit {execution.exit_code}
              </Badge>
            )}
            {execution.timed_out && <StatusBadge status="timed out" />}
            <span className="text-xs text-muted-foreground">
              {formatDuration(execution.duration_ms)}
            </span>
          </div>
          <code className="rounded bg-muted px-2 py-1 font-mono text-xs break-all">
            {list(execution.argv).join(" ")}
          </code>
          <span className="truncate font-mono text-xs text-muted-foreground">
            {execution.cwd}
          </span>
        </div>
      ))}
    </div>
  )
}

function Expandable({
  summary,
  children,
}: {
  summary: ReactNode
  children: ReactNode
}) {
  return (
    <details className="group rounded-md border">
      <summary className="flex cursor-pointer list-none items-start gap-2 p-3 [&::-webkit-details-marker]:hidden">
        <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">{summary}</div>
      </summary>
      <div className="border-t px-3 py-2">{children}</div>
    </details>
  )
}

function SnapshotSection({
  title,
  count,
  empty,
  children,
}: {
  title: string
  count: number
  empty: string
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-sm font-medium">
        {title}{" "}
        <span className="text-muted-foreground tabular-nums">{count}</span>
      </h3>
      {count === 0 ? <Empty>{empty}</Empty> : children}
    </section>
  )
}

function SnapshotView({ snapshot }: { snapshot: ContextSnapshot }) {
  const assembly = snapshot.metadata.assembly
  const usage = assembly?.usage
  const budget = assembly?.total_budget_bytes
  const used = usage?.total_bytes ?? 0
  const records = list(snapshot.records)
  const retrieval = list(snapshot.retrieval)
  const knowledge = list(snapshot.knowledge_catalog)

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap items-baseline justify-between gap-2 text-sm">
            <span className="font-medium">Context budget</span>
            <span className="text-muted-foreground tabular-nums">
              {formatBytes(used)} of {formatBytes(budget)}
            </span>
          </div>
          {budget ? (
            <Progress value={Math.min(100, (used / budget) * 100)} />
          ) : null}
          <div className="flex flex-wrap gap-6">
            <Field
              label="Durable context"
              value={formatBytes(usage?.durable_bytes)}
            />
            <Field
              label="Code retrieval"
              value={formatBytes(usage?.retrieval_bytes)}
            />
            <Field
              label="Snapshot schema"
              value={`v${snapshot.schema_version}`}
            />
          </div>
          {snapshot.metadata.override_reason && (
            <p className="text-sm">
              <span className="text-muted-foreground">Override reason: </span>
              {snapshot.metadata.override_reason}
            </p>
          )}
          {list(assembly?.warnings).map((warning) => (
            <p
              key={warning}
              className="text-sm text-amber-700 dark:text-amber-400"
            >
              {warning}
            </p>
          ))}
        </CardContent>
      </Card>

      <SnapshotSection
        title="Context records"
        count={records.length}
        empty="No saved project context was delivered to this run."
      >
        {records.map((record) => (
          <Expandable
            key={record.record_id}
            summary={
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium">{record.title}</span>
                  <Badge variant="outline" className="font-normal">
                    {record.kind}
                  </Badge>
                  {record.delivery && (
                    <Badge variant="secondary">{record.delivery}</Badge>
                  )}
                </div>
                {list(record.reasons).length > 0 && (
                  <span className="text-xs text-muted-foreground">
                    {list(record.reasons).join(" · ")}
                  </span>
                )}
              </>
            }
          >
            <Markdown>{record.snippet}</Markdown>
          </Expandable>
        ))}
      </SnapshotSection>

      <SnapshotSection
        title="Code retrieval"
        count={retrieval.length}
        empty="Retrieval was skipped or found no relevant code."
      >
        {retrieval.map((item) => (
          <Expandable
            key={item.item_id}
            summary={
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <code className="font-mono text-sm break-all">
                    {item.path}
                    <span className="text-muted-foreground">
                      :{item.line_start}-{item.line_end}
                    </span>
                  </code>
                  {item.symbol && (
                    <Badge
                      variant="secondary"
                      className="font-mono font-normal"
                    >
                      {item.symbol}
                    </Badge>
                  )}
                  <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                    {item.score.toFixed(3)}
                  </span>
                </div>
                {list(item.reasons).length > 0 && (
                  <span className="line-clamp-1 text-xs text-muted-foreground">
                    {list(item.reasons)[0]}
                  </span>
                )}
              </>
            }
          >
            <pre className="max-h-96 overflow-auto rounded bg-muted p-3 font-mono text-xs leading-relaxed">
              {item.snippet}
            </pre>
          </Expandable>
        ))}
      </SnapshotSection>

      <SnapshotSection
        title="Knowledge briefing"
        count={knowledge.length}
        empty="No current knowledge was briefed to this run."
      >
        {knowledge.map((item) => (
          <div key={item.id} className="rounded-md border p-3 text-sm">
            <div className="font-medium">{item.title}</div>
            {item.summary && (
              <div className="text-xs text-muted-foreground">
                {item.summary}
              </div>
            )}
          </div>
        ))}
      </SnapshotSection>
    </div>
  )
}

function LeaseAlert({ run }: { run: Run }) {
  return (
    <Alert className="border-amber-500/40 text-amber-800 dark:text-amber-300">
      <AlertTitle>Lease expired {formatRelative(run.expires_at)}</AlertTitle>
      <AlertDescription>
        The agent stopped sending heartbeats {formatRelative(run.heartbeat_at)}.
        The run can be recovered or abandoned.
      </AlertDescription>
    </Alert>
  )
}

export function RunPage() {
  const { runId = "" } = useParams()
  const now = useNow()
  const shown = useRun(runId)
  const projectId = shown.data?.snapshot.project_id ?? ""
  const project = useProject(projectId)
  const tasks = useTasks(projectId)

  const run = shown.data?.run
  const task = tasks.data?.find((item) => item.id === run?.task_id)
  const expired = run ? isRunStale(run, now) : false
  const validations = list(shown.data?.validations)
  const executions = list(shown.data?.executions)

  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Projects", to: "/" },
          {
            label: project.data?.name ?? "Project",
            to: projectId ? `/projects/${projectId}` : undefined,
          },
          ...(task
            ? [
                {
                  label: `#${task.number}`,
                  to: `/projects/${projectId}/tasks/${task.number}`,
                },
              ]
            : []),
          { label: "Run" },
        ]}
        title={
          <span className="flex flex-wrap items-center gap-3">
            Run by {run?.actor.name ?? "…"}
            {run &&
              (expired ? (
                <RunStateBadge active expired />
              ) : (
                <StatusBadge status={run.status} />
              ))}
          </span>
        }
      />

      <QueryState isPending={shown.isPending} error={shown.error}>
        {run && (
          <div className="flex flex-col gap-6">
            {task && (
              <Link
                to={`/projects/${projectId}/tasks/${task.number}`}
                className="-mt-3 flex min-w-0 items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
              >
                <span className="font-mono">#{task.number}</span>
                <span className="truncate">
                  <InlineCode text={task.title} />
                </span>
              </Link>
            )}

            {expired && <LeaseAlert run={run} />}

            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Result</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-4">
                {run.result_summary ? (
                  <Markdown>{run.result_summary}</Markdown>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    The run is still in progress.
                  </p>
                )}
                <div className="grid gap-4 sm:grid-cols-4">
                  <Field
                    label="Started"
                    value={formatDateTime(run.started_at)}
                  />
                  <Field
                    label="Duration"
                    value={formatDuration(runDurationMs(run, now))}
                  />
                  <Field
                    label="Base"
                    value={[run.base_branch, run.base_commit.slice(0, 10)]
                      .filter(Boolean)
                      .join(" @ ")}
                  />
                  <Field
                    label={
                      run.status === "active" ? "Last heartbeat" : "Finished by"
                    }
                    value={
                      run.status === "active"
                        ? formatRelative(run.heartbeat_at)
                        : run.finished_by?.name
                    }
                  />
                </div>
                {run.validation_override && (
                  <p className="text-sm">
                    <span className="text-muted-foreground">
                      Validation override:{" "}
                    </span>
                    {run.validation_override}
                  </p>
                )}
              </CardContent>
            </Card>

            <Tabs defaultValue="validations">
              <TabsList>
                <TabsTrigger value="validations">
                  Validations
                  <span className="ml-1 text-xs text-muted-foreground tabular-nums">
                    {validations.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="executions">
                  Executions
                  <span className="ml-1 text-xs text-muted-foreground tabular-nums">
                    {executions.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="context">What the agent saw</TabsTrigger>
              </TabsList>
              <TabsContent value="validations" className="pt-4">
                <ValidationList validations={validations} />
              </TabsContent>
              <TabsContent value="executions" className="pt-4">
                <ExecutionList executions={executions} />
              </TabsContent>
              <TabsContent value="context" className="pt-4">
                {shown.data && <SnapshotView snapshot={shown.data.snapshot} />}
              </TabsContent>
            </Tabs>
          </div>
        )}
      </QueryState>
    </>
  )
}

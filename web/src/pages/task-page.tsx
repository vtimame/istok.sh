import type { ReactNode } from "react"
import { Link, useParams } from "react-router"

import { InlineCode, Markdown } from "@/components/markdown"
import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { RunStateBadge, StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useProject, useRuns, useTask } from "@/hooks/queries"
import { useNow } from "@/hooks/use-now"
import { formatDateTime, formatDuration, formatRelative } from "@/lib/format"
import { isRunStale, runDurationMs } from "@/lib/runs"
import { list } from "@/lib/types"
import type { Run, TaskEvent, TaskSummary } from "@/lib/types"

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{title}</CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}

function TextSection({ title, body }: { title: string; body: string }) {
  if (!body.trim()) {
    return null
  }

  return (
    <Section title={title}>
      <Markdown>{body}</Markdown>
    </Section>
  )
}

function Detail({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right">{value}</span>
    </div>
  )
}

function TaskLinks({
  projectId,
  title,
  tasks,
}: {
  projectId: string
  title: string
  tasks: TaskSummary[]
}) {
  if (tasks.length === 0) {
    return null
  }

  return (
    <div className="flex flex-col gap-1.5 text-sm">
      <span className="text-xs text-muted-foreground">{title}</span>
      {tasks.map((task) => (
        <Link
          key={task.id}
          to={`/projects/${projectId}/tasks/${task.number}`}
          className="flex items-center gap-2 rounded-md px-1 py-0.5 hover:bg-muted"
        >
          <span className="font-mono text-xs text-muted-foreground">
            #{task.number}
          </span>
          <span className="min-w-0 flex-1 truncate">
            <InlineCode text={task.title} />
          </span>
          <StatusBadge status={task.status} />
        </Link>
      ))}
    </div>
  )
}

function RunCard({ run }: { run: Run }) {
  const now = useNow()
  const expired = isRunStale(run, now)

  return (
    <Link
      to={`/runs/${run.id}`}
      className="flex flex-col gap-1.5 rounded-md border p-3 text-sm transition-colors hover:bg-muted/50"
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{run.actor.name}</span>
        {expired ? (
          <RunStateBadge active expired />
        ) : (
          <StatusBadge status={run.status} />
        )}
      </div>
      <span className="text-xs text-muted-foreground">
        {formatRelative(run.started_at)} ·{" "}
        {formatDuration(runDurationMs(run, now))}
      </span>
      {run.result_summary && (
        <p className="line-clamp-3 text-xs text-muted-foreground">
          {run.result_summary}
        </p>
      )}
    </Link>
  )
}

function EventItem({ event }: { event: TaskEvent }) {
  return (
    <li className="relative flex flex-col gap-1 pb-5 pl-5 last:pb-0">
      <span className="absolute top-1.5 left-0 size-2 rounded-full bg-border ring-4 ring-card" />
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <Badge variant="outline" className="font-normal">
          {event.type.replaceAll("_", " ")}
        </Badge>
        <span className="font-medium text-foreground">{event.actor.name}</span>
        <span title={formatDateTime(event.created_at)}>
          {formatRelative(event.created_at)}
        </span>
      </div>
      {event.body && (
        <Markdown className="text-[0.8125rem]">{event.body}</Markdown>
      )}
    </li>
  )
}

export function TaskPage() {
  const { projectId = "", number = "" } = useParams()
  const project = useProject(projectId)
  const shown = useTask(projectId, number)
  const runs = useRuns(projectId, shown.data?.task.id ?? "")

  const task = shown.data?.task
  const events = [...list(shown.data?.events)].reverse()
  const blockers = list(shown.data?.blockers)
  const dependents = list(shown.data?.dependents)

  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Projects", to: "/" },
          {
            label: project.data?.name ?? "Project",
            to: `/projects/${projectId}`,
          },
          { label: `#${number}` },
        ]}
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span>
              {task ? <InlineCode text={task.title} /> : `Task #${number}`}
            </span>
            {task && <StatusBadge status={task.status} />}
            {shown.data && (
              <RunStateBadge
                active={shown.data.has_active_run}
                expired={shown.data.has_expired_run}
              />
            )}
          </span>
        }
      />

      <QueryState isPending={shown.isPending} error={shown.error}>
        {task && (
          <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
            <div className="flex min-w-0 flex-col gap-4">
              <TextSection title="Description" body={task.description} />
              <TextSection
                title="Acceptance criteria"
                body={task.acceptance_criteria}
              />
              <TextSection title="Notes" body={task.notes} />

              <Section title={`History (${events.length})`}>
                <ol className="relative before:absolute before:top-2 before:bottom-2 before:left-[3px] before:w-px before:bg-border">
                  {events.map((event) => (
                    <EventItem key={event.id} event={event} />
                  ))}
                </ol>
              </Section>
            </div>

            <div className="flex flex-col gap-4">
              <Section title="Details">
                <div className="flex flex-col gap-2">
                  <Detail
                    label="Number"
                    value={<span className="font-mono">#{task.number}</span>}
                  />
                  <Detail
                    label="Created"
                    value={formatDateTime(task.created_at)}
                  />
                  <Detail
                    label="Updated"
                    value={formatRelative(task.updated_at)}
                  />
                  <Detail label="Revision" value={task.revision} />
                </div>
              </Section>

              <Section title={`Runs (${runs.data?.length ?? 0})`}>
                <div className="flex flex-col gap-3">
                  {runs.data?.length === 0 && (
                    <p className="text-sm text-muted-foreground">
                      No runs yet.
                    </p>
                  )}
                  {runs.data?.map((run) => (
                    <RunCard key={run.id} run={run} />
                  ))}
                </div>
              </Section>

              <Section title="Dependencies">
                <div className="flex flex-col gap-4">
                  <TaskLinks
                    projectId={projectId}
                    title="Blocked by"
                    tasks={blockers}
                  />
                  <TaskLinks
                    projectId={projectId}
                    title="Blocks"
                    tasks={dependents}
                  />
                  {blockers.length === 0 && dependents.length === 0 && (
                    <p className="text-sm text-muted-foreground">None.</p>
                  )}
                </div>
              </Section>
            </div>
          </div>
        )}
      </QueryState>
    </>
  )
}

import { Link, useParams } from "react-router"

import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { StatusBadge } from "@/components/status-badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useProject, useRuns, useTask } from "@/hooks/queries"
import { formatDateTime, formatRelative } from "@/lib/format"
import { list } from "@/lib/types"
import type { TaskSummary } from "@/lib/types"

function TextSection({ title, body }: { title: string; body: string }) {
  if (!body.trim()) {
    return null
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{title}</CardTitle>
      </CardHeader>
      <CardContent className="text-sm whitespace-pre-wrap">{body}</CardContent>
    </Card>
  )
}

function TaskLinks({ projectId, title, tasks }: { projectId: string; title: string; tasks: TaskSummary[] }) {
  if (tasks.length === 0) {
    return null
  }

  return (
    <div className="flex flex-col gap-1 text-sm">
      <span className="text-muted-foreground">{title}</span>
      {tasks.map((task) => (
        <Link key={task.id} to={`/projects/${projectId}/tasks/${task.number}`} className="flex items-center gap-2 hover:underline">
          <span className="font-mono text-muted-foreground">#{task.number}</span>
          <span className="truncate">{task.title}</span>
          <StatusBadge status={task.status} />
        </Link>
      ))}
    </div>
  )
}

export function TaskPage() {
  const { projectId = "", number = "" } = useParams()
  const project = useProject(projectId)
  const shown = useTask(projectId, number)
  const runs = useRuns(projectId, shown.data?.task.id ?? "")

  const task = shown.data?.task
  const events = [...(shown.data?.events ?? [])].reverse()

  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Projects", to: "/" },
          { label: project.data?.name ?? "Project", to: `/projects/${projectId}` },
          { label: `#${number}` },
        ]}
        title={
          <span className="flex items-center gap-3">
            {task?.title ?? `Task #${number}`}
            {task && <StatusBadge status={task.status} />}
          </span>
        }
      />

      <QueryState isPending={shown.isPending} error={shown.error}>
        {task && shown.data && (
          <div className="grid gap-6 lg:grid-cols-[1fr_320px]">
            <div className="flex flex-col gap-4">
              <TextSection title="Description" body={task.description} />
              <TextSection title="Acceptance criteria" body={task.acceptance_criteria} />
              <TextSection title="Notes" body={task.notes} />

              <Card>
                <CardHeader>
                  <CardTitle className="text-sm">History</CardTitle>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                  {events.map((event) => (
                    <div key={event.id} className="flex flex-col gap-1 border-l-2 pl-3 text-sm">
                      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <span className="font-medium text-foreground">{event.type}</span>
                        <span>{event.actor.name}</span>
                        <span title={formatDateTime(event.created_at)}>{formatRelative(event.created_at)}</span>
                      </div>
                      {event.body && <p className="whitespace-pre-wrap">{event.body}</p>}
                    </div>
                  ))}
                </CardContent>
              </Card>
            </div>

            <div className="flex flex-col gap-4">
              <Card>
                <CardHeader>
                  <CardTitle className="text-sm">Runs</CardTitle>
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                  {runs.data?.length === 0 && <p className="text-sm text-muted-foreground">No runs yet.</p>}
                  {runs.data?.map((run) => (
                    <Link key={run.id} to={`/runs/${run.id}`} className="flex flex-col gap-1 rounded-md border p-3 text-sm hover:bg-muted/50">
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-medium">{run.actor.name}</span>
                        <StatusBadge status={run.status} />
                      </div>
                      <span className="text-xs text-muted-foreground">{formatDateTime(run.started_at)}</span>
                      {run.result_summary && <p className="line-clamp-3 text-xs">{run.result_summary}</p>}
                    </Link>
                  ))}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle className="text-sm">Dependencies</CardTitle>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                  <TaskLinks projectId={projectId} title="Blocked by" tasks={list(shown.data.blockers)} />
                  <TaskLinks projectId={projectId} title="Blocks" tasks={list(shown.data.dependents)} />
                  {list(shown.data.blockers).length === 0 && list(shown.data.dependents).length === 0 && (
                    <p className="text-sm text-muted-foreground">None.</p>
                  )}
                </CardContent>
              </Card>
            </div>
          </div>
        )}
      </QueryState>
    </>
  )
}

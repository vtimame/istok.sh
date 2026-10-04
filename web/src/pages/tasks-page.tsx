import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router"

import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useProject, useTasks } from "@/hooks/queries"
import { formatRelative } from "@/lib/format"
import type { TaskListItem } from "@/lib/types"

type Filter = "active" | "open" | "blocked" | "done" | "all"

function matches(task: TaskListItem, filter: Filter): boolean {
  switch (filter) {
    case "active":
      return task.status !== "done"
    case "all":
      return true
    default:
      return task.status === filter
  }
}

export function TasksPage() {
  const { projectId = "" } = useParams()
  const navigate = useNavigate()
  const project = useProject(projectId)
  const tasks = useTasks(projectId)
  const [filter, setFilter] = useState<Filter>("active")
  const [text, setText] = useState("")
  const needle = text.trim().toLowerCase()

  const visible = (tasks.data ?? [])
    .filter((task) => matches(task, filter))
    .filter((task) => !needle || `#${task.number} ${task.title}`.toLowerCase().includes(needle))
    .sort((left, right) => right.number - left.number)

  return (
    <>
      <PageHeader
        crumbs={[{ label: "Projects", to: "/" }, { label: project.data?.name ?? "Project" }]}
        title={project.data?.name ?? "Tasks"}
        actions={
          <Button variant="outline" asChild>
            <Link to={`/projects/${projectId}/knowledge`}>Knowledge</Link>
          </Button>
        }
      />

      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Tabs value={filter} onValueChange={(value) => setFilter(value as Filter)}>
          <TabsList>
          <TabsTrigger value="active">Not done</TabsTrigger>
          <TabsTrigger value="open">Open</TabsTrigger>
          <TabsTrigger value="blocked">Blocked</TabsTrigger>
          <TabsTrigger value="done">Done</TabsTrigger>
          <TabsTrigger value="all">All</TabsTrigger>
          </TabsList>
        </Tabs>
        <Input
          value={text}
          onChange={(event) => setText(event.target.value)}
          placeholder="Filter by title or #number"
          className="w-64"
        />
      </div>

      <QueryState isPending={tasks.isPending} error={tasks.error}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-20">#</TableHead>
              <TableHead>Title</TableHead>
              <TableHead className="w-28">Status</TableHead>
              <TableHead className="w-32">Updated</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visible.map((task) => (
              <TableRow
                key={task.id}
                className="cursor-pointer"
                onClick={() => navigate(`/projects/${projectId}/tasks/${task.number}`)}
              >
                <TableCell className="font-mono text-muted-foreground">{task.number}</TableCell>
                <TableCell className="whitespace-normal">
                  <div className="flex flex-wrap items-center gap-2">
                    <span>{task.title}</span>
                    {task.has_active_run && <Badge>agent working</Badge>}
                    {task.has_expired_run && <Badge variant="destructive">run expired</Badge>}
                  </div>
                </TableCell>
                <TableCell>
                  <StatusBadge status={task.status} />
                </TableCell>
                <TableCell className="text-muted-foreground">{formatRelative(task.updated_at)}</TableCell>
              </TableRow>
            ))}
            {visible.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="text-center text-muted-foreground">
                  No tasks.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </QueryState>
    </>
  )
}

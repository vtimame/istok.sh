import { useState } from "react"
import { Download, FolderInput, MoreHorizontal, Trash2 } from "lucide-react"
import { Link, useNavigate, useParams } from "react-router"

import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { BindFolderDialog } from "@/components/bind-folder-dialog"
import { DeleteProjectDialog } from "@/components/delete-project-dialog"
import { ProjectMissing } from "@/components/project-missing"
import { InlineCode } from "@/components/markdown"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { RunStateBadge, StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
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

const filters: Filter[] = ["active", "open", "blocked", "done", "all"]

const filterLabels: Record<Filter, string> = {
  active: "Not done",
  open: "Open",
  blocked: "Blocked",
  done: "Done",
  all: "All",
}

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
  const [deleting, setDeleting] = useState(false)
  const [binding, setBinding] = useState(false)
  const needle = text.trim().toLowerCase()

  const all = tasks.data ?? []
  const counts = Object.fromEntries(
    filters.map((key) => [key, all.filter((task) => matches(task, key)).length])
  ) as Record<Filter, number>

  const visible = all
    .filter((task) => matches(task, filter))
    .filter(
      (task) =>
        !needle ||
        `#${task.number} ${task.title}`.toLowerCase().includes(needle)
    )
    .sort((left, right) => right.number - left.number)

  if (project.isSuccess && !project.data) {
    return <ProjectMissing />
  }

  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Projects", to: "/" },
          { label: project.data?.name ?? "Project" },
        ]}
        title={project.data?.name ?? "Tasks"}
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" asChild>
              <Link to={`/projects/${projectId}/knowledge`}>Knowledge</Link>
            </Button>
            {project.data && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Project actions"
                  >
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                {/* The shadcn default matches the trigger width, too narrow for an icon button. */}
                <DropdownMenuContent align="end" className="w-auto min-w-44">
                  <DropdownMenuItem asChild>
                    <a href={`/api/v1/export?project=${projectId}`} download>
                      <Download />
                      Export project
                    </a>
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => setBinding(true)}>
                    <FolderInput />
                    {project.data.root ? "Change folder…" : "Bind folder…"}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() => setDeleting(true)}
                  >
                    <Trash2 />
                    Delete project…
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        }
      />

      {project.data && !project.data.root && (
        <Alert className="mb-4">
          <FolderInput />
          <AlertTitle>This project is not on this device</AlertTitle>
          <AlertDescription>
            <p>
              Tasks and history are here, but agents need the repository to work
              on it. Clone the repository, then bind its folder.
            </p>
            <Button
              size="sm"
              variant="outline"
              className="mt-2"
              onClick={() => setBinding(true)}
            >
              Bind folder…
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Tabs
          value={filter}
          onValueChange={(value) => setFilter(value as Filter)}
        >
          <TabsList>
            {filters.map((key) => (
              <TabsTrigger key={key} value={key}>
                {filterLabels[key]}
                <span className="ml-1 text-xs text-muted-foreground tabular-nums">
                  {counts[key]}
                </span>
              </TabsTrigger>
            ))}
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
                onClick={() =>
                  navigate(`/projects/${projectId}/tasks/${task.number}`)
                }
              >
                <TableCell className="font-mono text-muted-foreground">
                  {task.number}
                </TableCell>
                <TableCell className="whitespace-normal">
                  <div className="flex flex-wrap items-center gap-2">
                    <span>
                      <InlineCode text={task.title} />
                    </span>
                    <RunStateBadge
                      active={task.has_active_run}
                      expired={task.has_expired_run}
                    />
                  </div>
                </TableCell>
                <TableCell>
                  <StatusBadge status={task.status} />
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {formatRelative(task.updated_at)}
                </TableCell>
              </TableRow>
            ))}
            {visible.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className="text-center text-muted-foreground"
                >
                  No tasks.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </QueryState>
      {project.data && (
        <>
          <DeleteProjectDialog
            project={project.data}
            open={deleting}
            onOpenChange={setDeleting}
          />
          <BindFolderDialog
            project={project.data}
            open={binding}
            onOpenChange={setBinding}
          />
        </>
      )}
    </>
  )
}

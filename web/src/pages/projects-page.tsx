import { useState } from "react"
import { Download } from "lucide-react"
import { Link } from "react-router"

import { ImportDialog } from "@/components/import-dialog"
import { PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { QueryState } from "@/components/query-state"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useHealth, useProjects } from "@/hooks/queries"
import { formatRelative, shortenPath } from "@/lib/format"
import type { Project } from "@/lib/types"

type SortKey = "activity" | "name" | "created"

const SORT_STORAGE_KEY = "istok.projects.sort"

function activityTime(project: Project): number {
  return new Date(
    project.stats.last_activity_at ?? project.created_at
  ).getTime()
}

const comparators: Record<SortKey, (left: Project, right: Project) => number> =
  {
    activity: (left, right) => activityTime(right) - activityTime(left),
    name: (left, right) => left.name.localeCompare(right.name),
    created: (left, right) =>
      new Date(right.created_at).getTime() -
      new Date(left.created_at).getTime(),
  }

function isSortKey(value: string | null): value is SortKey {
  return value === "activity" || value === "name" || value === "created"
}

function readSort(): SortKey {
  try {
    const stored = localStorage.getItem(SORT_STORAGE_KEY)
    return isSortKey(stored) ? stored : "activity"
  } catch {
    return "activity"
  }
}

function Counter({
  value,
  label,
  className,
}: {
  value: number
  label: string
  className?: string
}) {
  return (
    <span className={value > 0 ? className : "text-muted-foreground/60"}>
      <span className="font-medium tabular-nums">{value}</span> {label}
    </span>
  )
}

function ProjectCard({ project, home }: { project: Project; home?: string }) {
  const { stats } = project
  const path = project.root?.canonical_path

  return (
    <Link to={`/projects/${project.id}`}>
      <Card className="h-full gap-3 transition-colors hover:border-foreground/20 hover:bg-muted/40">
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="truncate">{project.name}</CardTitle>
            <div className="flex shrink-0 items-center gap-3 text-xs">
              {stats.active_runs > 0 && (
                <span className="flex items-center gap-1.5 text-sky-700 dark:text-sky-400">
                  <span className="size-1.5 animate-pulse rounded-full bg-sky-500" />
                  {stats.active_runs} running
                </span>
              )}
              {stats.stale_runs > 0 && (
                <span
                  className="text-amber-700 dark:text-amber-400"
                  title="Active runs whose agent stopped sending heartbeats"
                >
                  {stats.stale_runs} stale
                </span>
              )}
            </div>
          </div>
          {path ? (
            <CardDescription
              className="truncate font-mono text-xs"
              title={path}
            >
              {shortenPath(path, home)}
            </CardDescription>
          ) : (
            <CardDescription className="text-xs text-amber-700 dark:text-amber-400">
              Not on this device: bind its folder
            </CardDescription>
          )}
        </CardHeader>
        <CardContent className="mt-auto flex items-center justify-between gap-2 text-xs">
          <div className="flex gap-3">
            <Counter value={stats.open} label="open" />
            <Counter
              value={stats.blocked}
              label="blocked"
              className="text-red-700 dark:text-red-400"
            />
            <Counter
              value={stats.done}
              label="done"
              className="text-muted-foreground"
            />
          </div>
          <span className="shrink-0 text-muted-foreground">
            {stats.last_activity_at
              ? formatRelative(stats.last_activity_at)
              : "no tasks"}
          </span>
        </CardContent>
      </Card>
    </Link>
  )
}

export function ProjectsPage() {
  const projects = useProjects()
  const health = useHealth()
  const [sort, setSort] = useState<SortKey>(readSort)

  const changeSort = (value: string) => {
    if (!isSortKey(value)) {
      return
    }

    setSort(value)
    try {
      localStorage.setItem(SORT_STORAGE_KEY, value)
    } catch {
      // Storage can be unavailable; the choice then lasts for this page view.
    }
  }

  const sorted = [...(projects.data ?? [])].sort(comparators[sort])

  return (
    <>
      <PageHeader
        crumbs={[{ label: "Projects" }]}
        title="Projects"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Tabs value={sort} onValueChange={changeSort}>
              <TabsList>
                <TabsTrigger value="activity">Recent activity</TabsTrigger>
                <TabsTrigger value="name">Name</TabsTrigger>
                <TabsTrigger value="created">Created</TabsTrigger>
              </TabsList>
            </Tabs>
            <ImportDialog />
            {sorted.length > 0 && (
              <Button variant="outline" asChild>
                <a href="/api/v1/export?all=1" download>
                  <Download />
                  Export all
                </a>
              </Button>
            )}
          </div>
        }
      />

      <QueryState isPending={projects.isPending} error={projects.error}>
        {sorted.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No projects yet. Run <code>istok init</code> in a repository.
          </p>
        )}

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {sorted.map((project) => (
            <ProjectCard
              key={project.id}
              project={project}
              home={health.data?.home}
            />
          ))}
        </div>
      </QueryState>
    </>
  )
}

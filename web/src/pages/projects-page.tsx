import { useState } from "react"
import { Link } from "react-router"

import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useProjects } from "@/hooks/queries"
import { formatRelative } from "@/lib/format"
import type { Project } from "@/lib/types"

type SortKey = "activity" | "name" | "created"

const SORT_STORAGE_KEY = "istok.projects.sort"

function activityTime(project: Project): number {
  return new Date(project.last_activity_at ?? project.created_at).getTime()
}

const comparators: Record<SortKey, (left: Project, right: Project) => number> = {
  activity: (left, right) => activityTime(right) - activityTime(left),
  name: (left, right) => left.name.localeCompare(right.name),
  created: (left, right) => new Date(right.created_at).getTime() - new Date(left.created_at).getTime(),
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

export function ProjectsPage() {
  const projects = useProjects()
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
          <Tabs value={sort} onValueChange={changeSort}>
            <TabsList>
              <TabsTrigger value="activity">Recent activity</TabsTrigger>
              <TabsTrigger value="name">Name</TabsTrigger>
              <TabsTrigger value="created">Created</TabsTrigger>
            </TabsList>
          </Tabs>
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
            <Link key={project.id} to={`/projects/${project.id}`}>
              <Card className="h-full transition-colors hover:bg-muted/50">
                <CardHeader>
                  <CardTitle>{project.name}</CardTitle>
                  <CardDescription className="truncate font-mono text-xs" title={project.root?.canonical_path}>
                    {project.root?.canonical_path ?? "detached"}
                  </CardDescription>
                  <CardDescription className="text-xs">
                    {project.last_activity_at
                      ? `Active ${formatRelative(project.last_activity_at)}`
                      : `No tasks · created ${formatRelative(project.created_at)}`}
                  </CardDescription>
                </CardHeader>
              </Card>
            </Link>
          ))}
        </div>
      </QueryState>
    </>
  )
}

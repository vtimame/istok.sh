import { Link } from "react-router"

import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useProjects } from "@/hooks/queries"
import { formatRelative } from "@/lib/format"

export function ProjectsPage() {
  const projects = useProjects()

  return (
    <>
      <PageHeader crumbs={[{ label: "Projects" }]} title="Projects" />

      <QueryState isPending={projects.isPending} error={projects.error}>
        {projects.data?.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No projects yet. Run <code>istok init</code> in a repository.
          </p>
        )}

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {projects.data?.map((project) => (
            <Link key={project.id} to={`/projects/${project.id}`}>
              <Card className="h-full transition-colors hover:bg-muted/50">
                <CardHeader>
                  <CardTitle>{project.name}</CardTitle>
                  <CardDescription className="truncate font-mono text-xs" title={project.root?.canonical_path}>
                    {project.root?.canonical_path ?? "detached"}
                  </CardDescription>
                  <CardDescription className="text-xs">
                    Updated {formatRelative(project.updated_at)}
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

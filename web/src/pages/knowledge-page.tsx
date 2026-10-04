import { useParams } from "react-router"

import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { useKnowledge, useProject } from "@/hooks/queries"
import { formatRelative } from "@/lib/format"
import { list } from "@/lib/types"

export function KnowledgePage() {
  const { projectId = "" } = useParams()
  const project = useProject(projectId)
  const knowledge = useKnowledge(projectId)

  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Projects", to: "/" },
          {
            label: project.data?.name ?? "Project",
            to: `/projects/${projectId}`,
          },
          { label: "Knowledge" },
        ]}
        title="Knowledge"
      />

      <QueryState isPending={knowledge.isPending} error={knowledge.error}>
        {knowledge.data?.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No knowledge yet. Agents add drafts with{" "}
            <code>knowledge_distill</code>.
          </p>
        )}

        <div className="flex flex-col gap-3">
          {knowledge.data?.map((item) => (
            <Card key={item.id}>
              <CardHeader>
                <div className="flex flex-wrap items-center gap-2">
                  <CardTitle className="text-base">{item.title}</CardTitle>
                  <StatusBadge status={item.status} />
                  <Badge variant="outline">{item.kind}</Badge>
                  {list(item.tags).map((tag) => (
                    <Badge key={tag} variant="secondary">
                      {tag}
                    </Badge>
                  ))}
                </div>
                <CardDescription>
                  {item.summary || item.snippet}
                </CardDescription>
                <CardDescription className="text-xs">
                  Updated {formatRelative(item.updated_at)}
                  {item.reviewed_at
                    ? ` · reviewed ${formatRelative(item.reviewed_at)}`
                    : " · not reviewed"}
                </CardDescription>
              </CardHeader>
            </Card>
          ))}
        </div>
      </QueryState>
    </>
  )
}

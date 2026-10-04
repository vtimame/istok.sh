import type { ReactNode } from "react"
import { useParams } from "react-router"

import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useProject, useRun } from "@/hooks/queries"
import { formatBytes, formatDateTime, formatDuration } from "@/lib/format"
import type { ContextSnapshot, Execution, Validation } from "@/lib/types"

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-sm">{value || "—"}</span>
    </div>
  )
}

function ValidationList({ validations }: { validations: Validation[] }) {
  if (validations.length === 0) {
    return <p className="text-sm text-muted-foreground">No validation recorded.</p>
  }

  return (
    <div className="flex flex-col gap-3">
      {validations.map((validation) => (
        <div key={validation.id} className="flex flex-col gap-2 rounded-md border p-3">
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge status={validation.status} />
            <Badge variant="outline">{validation.source}</Badge>
            <code className="text-xs break-all">{validation.command}</code>
          </div>
          {validation.summary && <p className="text-sm whitespace-pre-wrap">{validation.summary}</p>}
          <span className="text-xs text-muted-foreground">
            {validation.actor.name} · {formatDateTime(validation.created_at)}
          </span>
        </div>
      ))}
    </div>
  )
}

function ExecutionList({ executions }: { executions: Execution[] }) {
  if (executions.length === 0) {
    return <p className="text-sm text-muted-foreground">No executions.</p>
  }

  return (
    <div className="flex flex-col gap-3">
      {executions.map((execution) => (
        <div key={execution.id} className="flex flex-col gap-2 rounded-md border p-3">
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge status={execution.status} />
            {execution.exit_code !== undefined && <Badge variant="outline">exit {execution.exit_code}</Badge>}
            {execution.timed_out && <Badge variant="destructive">timed out</Badge>}
            <span className="text-xs text-muted-foreground">{formatDuration(execution.duration_ms)}</span>
          </div>
          <code className="text-xs break-all">{execution.argv.join(" ")}</code>
          <span className="font-mono text-xs text-muted-foreground">{execution.cwd}</span>
        </div>
      ))}
    </div>
  )
}

function SnapshotView({ snapshot }: { snapshot: ContextSnapshot }) {
  const usage = snapshot.metadata.assembly?.usage

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-6">
        <Field label="Schema" value={`v${snapshot.schema_version}`} />
        <Field label="Durable context" value={formatBytes(usage?.durable_bytes)} />
        <Field label="Retrieval" value={formatBytes(usage?.retrieval_bytes)} />
        <Field label="Total" value={`${formatBytes(usage?.total_bytes)} / ${formatBytes(snapshot.metadata.assembly?.total_budget_bytes)}`} />
      </div>

      {snapshot.metadata.override_reason && (
        <p className="text-sm">
          <span className="text-muted-foreground">Override: </span>
          {snapshot.metadata.override_reason}
        </p>
      )}

      <section className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">Context records ({snapshot.records.length})</h3>
        {snapshot.records.map((record) => (
          <div key={record.record_id} className="flex flex-col gap-1 rounded-md border p-3">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="outline">{record.kind}</Badge>
              {record.delivery && <Badge variant="secondary">{record.delivery}</Badge>}
              <span className="text-sm font-medium">{record.title}</span>
            </div>
            <p className="line-clamp-3 text-xs text-muted-foreground">{record.snippet}</p>
          </div>
        ))}
      </section>

      <section className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">Code retrieval ({snapshot.retrieval.length})</h3>
        {snapshot.retrieval.map((item) => (
          <div key={item.item_id} className="flex flex-col gap-1 rounded-md border p-3">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <code className="break-all">
                {item.path}:{item.line_start}-{item.line_end}
              </code>
              {item.symbol && <Badge variant="outline">{item.symbol}</Badge>}
              <span className="text-xs text-muted-foreground">score {item.score.toFixed(3)}</span>
            </div>
            {item.reasons && item.reasons.length > 0 && (
              <p className="line-clamp-2 text-xs text-muted-foreground">{item.reasons.join(" · ")}</p>
            )}
          </div>
        ))}
      </section>

      <section className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">Knowledge briefing ({snapshot.knowledge_catalog.length})</h3>
        {snapshot.knowledge_catalog.map((item) => (
          <div key={item.id} className="rounded-md border p-3 text-sm">
            {item.title}
          </div>
        ))}
      </section>
    </div>
  )
}

export function RunPage() {
  const { runId = "" } = useParams()
  const shown = useRun(runId)
  const projectId = shown.data?.snapshot.project_id ?? ""
  const project = useProject(projectId)
  const run = shown.data?.run

  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Projects", to: "/" },
          { label: project.data?.name ?? "Project", to: projectId ? `/projects/${projectId}` : undefined },
          { label: "Run" },
        ]}
        title={
          <span className="flex items-center gap-3">
            Run by {run?.actor.name ?? "…"}
            {run && <StatusBadge status={run.status} />}
          </span>
        }
      />

      <QueryState isPending={shown.isPending} error={shown.error}>
        {run && shown.data && (
          <div className="flex flex-col gap-6">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Result</CardTitle>
                {run.result_summary ? (
                  <CardDescription className="text-sm whitespace-pre-wrap text-foreground">{run.result_summary}</CardDescription>
                ) : (
                  <CardDescription>Run is still in progress.</CardDescription>
                )}
              </CardHeader>
              <CardContent className="grid gap-4 sm:grid-cols-3">
                <Field label="Started" value={formatDateTime(run.started_at)} />
                <Field label="Finished" value={formatDateTime(run.finished_at)} />
                <Field label="Base" value={[run.base_branch, run.base_commit.slice(0, 10)].filter(Boolean).join(" @ ")} />
                {run.validation_override && <Field label="Validation override" value={run.validation_override} />}
              </CardContent>
            </Card>

            <Tabs defaultValue="validations">
              <TabsList>
                <TabsTrigger value="validations">Validations ({shown.data.validations.length})</TabsTrigger>
                <TabsTrigger value="executions">Executions ({shown.data.executions.length})</TabsTrigger>
                <TabsTrigger value="context">What the agent saw</TabsTrigger>
              </TabsList>
              <TabsContent value="validations" className="pt-4">
                <ValidationList validations={shown.data.validations} />
              </TabsContent>
              <TabsContent value="executions" className="pt-4">
                <ExecutionList executions={shown.data.executions} />
              </TabsContent>
              <TabsContent value="context" className="pt-4">
                <SnapshotView snapshot={shown.data.snapshot} />
              </TabsContent>
            </Tabs>
          </div>
        )}
      </QueryState>
    </>
  )
}

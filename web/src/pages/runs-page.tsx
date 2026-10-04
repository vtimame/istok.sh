import { useEffect, useState } from "react"
import { Search, X } from "lucide-react"
import { Link, useNavigate, useSearchParams } from "react-router"

import { AbandonRunDialog } from "@/components/abandon-run-dialog"
import { InlineCode } from "@/components/markdown"
import { PageHeader } from "@/components/page-header"
import { QueryState } from "@/components/query-state"
import { RunStateBadge, StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useProjects, useRunFeed } from "@/hooks/queries"
import { useDebouncedValue } from "@/hooks/use-debounced-value"
import { useNow } from "@/hooks/use-now"
import { formatDuration, formatRelative } from "@/lib/format"
import { isRunStale, runDurationMs } from "@/lib/runs"
import type { FeedItem } from "@/lib/types"

type Filter = "all" | "running" | "stale" | "succeeded" | "failed"

type FilterQuery = { statuses: string[]; lease: "" | "live" | "expired" }

// Running and stale both mean status=active; the lease tells whether the
// agent still heartbeats, so stale runs are the ones nobody finished.
const filterQueries: Record<Filter, FilterQuery> = {
  all: { statuses: [], lease: "" },
  running: { statuses: [], lease: "live" },
  stale: { statuses: [], lease: "expired" },
  succeeded: { statuses: ["succeeded"], lease: "" },
  failed: {
    statuses: ["failed", "blocked", "cancelled", "abandoned"],
    lease: "",
  },
}

const filterLabels: Record<Filter, string> = {
  all: "All",
  running: "Running",
  stale: "Stale",
  succeeded: "Succeeded",
  failed: "Failed & stopped",
}

function isFilter(value: string | null): value is Filter {
  return value !== null && value in filterLabels
}

const dayFormat = new Intl.DateTimeFormat("en-US", {
  weekday: "long",
  month: "long",
  day: "numeric",
})

function dayLabel(value: string, now: number): string {
  const date = new Date(value)
  const today = new Date(now)
  const yesterday = new Date(now - 86400000)

  if (date.toDateString() === today.toDateString()) {
    return "Today"
  }
  if (date.toDateString() === yesterday.toDateString()) {
    return "Yesterday"
  }

  return dayFormat.format(date)
}

// groupByDay keeps the newest-first order and splits runs into day sections.
function groupByDay(items: FeedItem[], now: number): [string, FeedItem[]][] {
  const groups: [string, FeedItem[]][] = []

  for (const item of items) {
    const label = dayLabel(item.run.started_at, now)
    const last = groups.at(-1)
    if (last && last[0] === label) {
      last[1].push(item)
    } else {
      groups.push([label, [item]])
    }
  }

  return groups
}

function ValidationSummary({
  passed,
  failed,
}: {
  passed: number
  failed: number
}) {
  if (passed === 0 && failed === 0) {
    return <span className="text-muted-foreground/60">no checks</span>
  }

  return (
    <span className="flex gap-2">
      {passed > 0 && (
        <span className="text-emerald-700 dark:text-emerald-400">
          {passed} passed
        </span>
      )}
      {failed > 0 && (
        <span className="text-red-700 dark:text-red-400">{failed} failed</span>
      )}
    </span>
  )
}

function FeedRow({
  item,
  now,
  showProject,
}: {
  item: FeedItem
  now: number
  showProject: boolean
}) {
  const navigate = useNavigate()
  const stale = isRunStale(item.run, now)
  const taskPath = `/projects/${item.project.id}/tasks/${item.task.number}`

  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => navigate(`/runs/${item.run.id}`)}
      onKeyDown={(event) =>
        event.key === "Enter" && navigate(`/runs/${item.run.id}`)
      }
      className="flex cursor-pointer flex-col gap-1.5 border-b px-3 py-3 transition-colors last:border-b-0 hover:bg-muted/40"
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        {stale ? (
          <RunStateBadge active expired />
        ) : (
          <StatusBadge status={item.run.status} />
        )}
        <span className="font-medium">{item.run.actor_name}</span>
        {showProject && (
          <Link
            to={`/projects/${item.project.id}`}
            onClick={(event) => event.stopPropagation()}
            className="text-muted-foreground hover:text-foreground hover:underline"
          >
            {item.project.name}
          </Link>
        )}
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">
          {formatRelative(item.run.started_at)} ·{" "}
          {stale
            ? `last heartbeat ${formatRelative(item.run.heartbeat_at)}`
            : formatDuration(runDurationMs(item.run, now))}
        </span>
        {stale && (
          <AbandonRunDialog
            runId={item.run.id}
            revision={item.run.revision}
            actorName={item.run.actor_name}
            heartbeatAt={item.run.heartbeat_at}
          />
        )}
      </div>

      <Link
        to={taskPath}
        onClick={(event) => event.stopPropagation()}
        className="flex min-w-0 items-center gap-2 text-sm hover:underline"
      >
        <span className="font-mono text-xs text-muted-foreground">
          #{item.task.number}
        </span>
        <span className="truncate">
          <InlineCode text={item.task.title} />
        </span>
      </Link>

      {item.run.result_summary && (
        <p className="line-clamp-2 text-xs text-muted-foreground">
          <InlineCode text={item.run.result_summary} />
        </p>
      )}

      <div className="text-xs">
        <ValidationSummary
          passed={item.validations.passed}
          failed={item.validations.failed}
        />
      </div>
    </div>
  )
}

// projectMatches is computed here rather than in SQL: JavaScript lowercases
// Unicode names correctly, while SQLite lower() only handles ASCII.
function projectMatches(name: string, query: string): boolean {
  return name.toLocaleLowerCase().includes(query.toLocaleLowerCase())
}

export function RunsPage() {
  const now = useNow()
  const projects = useProjects()
  const [params, setParams] = useSearchParams()

  const rawFilter = params.get("status")
  const filter: Filter = isFilter(rawFilter) ? rawFilter : "all"
  const projectQuery = (params.get("project_q") ?? "").trim()
  const [text, setText] = useState(projectQuery)
  const debouncedText = useDebouncedValue(text.trim(), 250)

  const update = (key: string, value: string, fallback: string) => {
    const next = new URLSearchParams(params)
    if (value === fallback) {
      next.delete(key)
    } else {
      next.set(key, value)
    }
    setParams(next, { replace: true })
  }

  // The URL follows the debounced input, so filters survive reloads and links.
  useEffect(() => {
    if (debouncedText !== projectQuery) {
      update("project_q", debouncedText, "")
    }
    // update reads the latest params; only the debounced text drives this.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedText])

  const matchingProjects = projectQuery
    ? (projects.data ?? []).filter((project) =>
        projectMatches(project.name, projectQuery)
      )
    : []
  const noMatches =
    projectQuery !== "" && projects.isSuccess && matchingProjects.length === 0

  const feed = useRunFeed(
    matchingProjects.map((project) => project.id),
    filterQueries[filter].statuses,
    filterQueries[filter].lease,
    projectQuery === "" || (projects.isSuccess && !noMatches)
  )

  const items = feed.data?.pages.flat() ?? []

  return (
    <>
      <PageHeader crumbs={[{ label: "Runs" }]} title="Runs" />

      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Tabs
          value={filter}
          onValueChange={(value) => update("status", value, "all")}
        >
          <TabsList>
            {(Object.keys(filterLabels) as Filter[]).map((key) => (
              <TabsTrigger key={key} value={key}>
                {filterLabels[key]}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <div className="relative w-64">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={text}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => event.key === "Escape" && setText("")}
            placeholder="Filter by project"
            aria-label="Filter runs by project name"
            className="pr-8 pl-8"
          />
          {text && (
            <button
              type="button"
              onClick={() => setText("")}
              aria-label="Clear project filter"
              className="absolute top-1/2 right-2 -translate-y-1/2 rounded text-muted-foreground hover:text-foreground"
            >
              <X className="size-4" />
            </button>
          )}
        </div>
      </div>

      {projectQuery && matchingProjects.length > 0 && (
        <p className="mb-4 text-xs text-muted-foreground">
          {matchingProjects.length === 1
            ? "1 project matches"
            : `${matchingProjects.length} projects match`}
          : {matchingProjects.map((project) => project.name).join(", ")}
        </p>
      )}

      {noMatches ? (
        <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
          No project name contains “{projectQuery}”.
        </p>
      ) : (
        <QueryState isPending={feed.isPending} error={feed.error}>
          {items.length === 0 && (
            <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
              No runs match these filters.
            </p>
          )}

          <div className="flex flex-col gap-6">
            {groupByDay(items, now).map(([label, group]) => (
              <section key={label} className="flex flex-col gap-2">
                <h2 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                  {label}
                </h2>
                <div className="rounded-lg border">
                  {group.map((item) => (
                    <FeedRow
                      key={item.run.id}
                      item={item}
                      now={now}
                      showProject={matchingProjects.length !== 1}
                    />
                  ))}
                </div>
              </section>
            ))}
          </div>

          {feed.hasNextPage && (
            <div className="mt-6 flex justify-center">
              <Button
                variant="outline"
                onClick={() => feed.fetchNextPage()}
                disabled={feed.isFetchingNextPage}
              >
                {feed.isFetchingNextPage ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </QueryState>
      )}
    </>
  )
}

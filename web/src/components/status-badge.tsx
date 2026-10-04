import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

const success =
  "border-transparent bg-emerald-500/15 text-emerald-700 dark:text-emerald-400"
const running =
  "border-transparent bg-sky-500/15 text-sky-700 dark:text-sky-400"
const danger = "border-transparent bg-red-500/15 text-red-700 dark:text-red-400"
const warning =
  "border-transparent bg-amber-500/15 text-amber-700 dark:text-amber-400"
const muted = "border-transparent bg-muted text-muted-foreground"

const tones: Record<string, string> = {
  open: "",
  blocked: danger,
  done: success,
  active: running,
  running: running,
  succeeded: success,
  passed: success,
  current: success,
  failed: danger,
  cancelled: muted,
  abandoned: muted,
  draft: warning,
  superseded: muted,
  stale: warning,
}

export function StatusBadge({
  status,
  className,
}: {
  status: string
  className?: string
}) {
  return (
    <Badge variant="outline" className={cn(tones[status], className)}>
      {status}
    </Badge>
  )
}

// RunStateBadge summarizes a task's run state; an active run whose lease has
// expired is shown as stale rather than as both working and expired.
export function RunStateBadge({
  active,
  expired,
}: {
  active: boolean
  expired: boolean
}) {
  if (expired) {
    return <StatusBadge status="stale" className="gap-1" />
  }
  if (!active) {
    return null
  }

  return (
    <Badge variant="outline" className={cn(running, "gap-1.5")}>
      <span className="size-1.5 animate-pulse rounded-full bg-sky-500" />
      agent working
    </Badge>
  )
}
